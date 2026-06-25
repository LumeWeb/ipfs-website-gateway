package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alexliesenfeld/health"
	"github.com/labstack/echo-contrib/v5/echoprometheus"
	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/config"
	gw "go.lumeweb.com/ipfs-website-gateway/internal/gateway"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

// IP extractor trust options for secure IP extraction
var trustOptions = []echo.TrustOption{
	echo.TrustLoopback(true),
	echo.TrustLinkLocal(true),
	echo.TrustPrivateNet(true),
}

// Server wraps an Echo instance with application-specific configuration.
type Server struct {
	echo         *echo.Echo
	config       *config.Config
	logger       *zap.Logger
	dns          DNSValidator
	api          api.APIClient
	statusCache  StatusCache
	healthChecker health.Checker
	gateway      *gw.Gateway
}

// NewServer creates and configures a new Echo server instance.
// It sets up IP extraction for Caddy proxy, adds middleware for recovery,
// logging, and real IP detection, and initializes routes.
func NewServer(cfg *config.Config, logger *zap.Logger) *Server {
	e := echo.New()

	// Configure IP extractor - using X-Real-IP first (for Caddy), then X-Forwarded-For
	e.IPExtractor = func(r *http.Request) string {
		// Try X-Real-IP header first (Caddy sets this)
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			if ip := echo.ExtractIPFromRealIPHeader(trustOptions...)(r); ip != "" {
				return ip
			}
		}
		// Fall back to X-Forwarded-For header
		return echo.ExtractIPFromXFFHeader(trustOptions...)(r)
	}

	srv := &Server{
		echo:   e,
		config: cfg,
		logger: logger,
	}

	srv.setupMiddleware(e)
	return srv
}

// setupMiddleware initializes global middleware for the server.
func (s *Server) setupMiddleware(e *echo.Echo) {
	e.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
		Registerer: metrics.Registerer(),
	}))
	e.Use(echoMiddleware.Recover())
	e.Use(echoMiddleware.RequestLoggerWithConfig(echoMiddleware.RequestLoggerConfig{
		LogMethod:    true,
		LogURI:       true,
		LogStatus:    true,
		LogRemoteIP:  true,
		LogValuesFunc: func(c *echo.Context, v echoMiddleware.RequestLoggerValues) error {
			s.logger.Info("request",
				zap.String("method", v.Method),
				zap.String("uri", v.URI),
				zap.Int("status", v.Status),
				zap.String("remote_ip", v.RemoteIP),
			)
			return nil
		},
	}))
}

// InitializeRoutes initializes HTTP routes for the server.
// This method should be called after all dependencies are set.
func (s *Server) InitializeRoutes() {
	s.setupRoutes(s.echo)
}

// setupRoutes initializes HTTP routes for the server.
func (s *Server) setupRoutes(e *echo.Echo) {
	e.GET("/healthz", s.healthCheckHandler)

	if s.config.Observability.IsMetricsEnabled() {
		metricsPath := s.config.Observability.Metrics.Path
		if metricsPath == "" {
			metricsPath = "/metrics"
		}

		handler := echoprometheus.NewHandlerWithConfig(echoprometheus.HandlerConfig{
			Gatherer: metrics.Registry(),
		})

		if s.config.Observability.Metrics.IsBasicAuthEnabled() {
			e.GET(metricsPath, handler, echoMiddleware.BasicAuth(func(c *echo.Context, username, password string) (bool, error) {
				return subtle.ConstantTimeCompare([]byte(password), []byte(s.config.Observability.Metrics.BasicAuthPassword)) == 1, nil
			}))
		} else {
			e.GET(metricsPath, handler)
		}

		s.logger.Info("Metrics endpoint enabled", zap.String("path", metricsPath))
	}

	if s.config.RateLimit.Enabled {
		store := echoMiddleware.NewRateLimiterMemoryStoreWithConfig(
			echoMiddleware.RateLimiterMemoryStoreConfig{
				Rate:      s.config.RateLimit.Rate,
				Burst:     s.config.RateLimit.Burst,
				ExpiresIn: s.config.RateLimit.ExpiresIn,
			},
		)

		denyHandler := func(c *echo.Context, identifier string, err error) error {
			c.Response().Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.2f", s.config.RateLimit.Rate))
			c.Response().Header().Set("X-RateLimit-Burst", fmt.Sprintf("%d", s.config.RateLimit.Burst))

			s.logger.Warn("rate limit exceeded",
				zap.String("ip", identifier),
			)

			return c.JSON(http.StatusTooManyRequests, map[string]string{
				"error": "rate limit exceeded",
			})
		}

		rateLimitMiddleware := echoMiddleware.RateLimiterWithConfig(echoMiddleware.RateLimiterConfig{
			Store:       store,
			DenyHandler: denyHandler,
		})

		e.GET("/allowed", rateLimitMiddleware(s.authMiddleware(s.allowedHandler)))
	} else {
		e.GET("/allowed", s.authMiddleware(s.allowedHandler))
	}

	if s.gateway != nil {
		gatewayHandler := s.gateway.Handler()
		cacheControl := gw.NewCacheControlMiddleware()
		accessControl, err := gw.NewAccessControlMiddleware(s.gateway, s.logger)
		if err != nil {
			s.logger.Fatal("failed to create access control middleware", zap.Error(err))
		}
		wrappedHandler := accessControl.Wrap(cacheControl.Wrap(gatewayHandler))

		e.Any("/*", echo.WrapHandler(wrappedHandler))
	}
}

// allowedHandler handles Caddy On-Demand TLS requests to validate domains.
// This endpoint is called by Caddy before issuing SSL certificates.
//
// It validates the domain parameter from the query string and returns:
//   - 200 OK: if the domain is valid and authorized
//   - 400 Bad Request: for all failures (auth, domain validation, DNSLink, API)
//
// SECURITY: Always returns 400 on failure to prevent information leakage.
func (s *Server) allowedHandler(c *echo.Context) (err error) {
	ctx := c.Request().Context()
	ctx, span := otel.TraceMethod(ctx, "Server.allowedHandler",
		otel.WithAttributes(
			attribute.String("domain", c.QueryParam("domain")),
			attribute.String("client_ip", c.RealIP()),
		),
	)
	defer func() { otel.EndSpanWithErr(span, err) }()

	domain := c.QueryParam("domain")

	s.logger.Debug("Caddy On-Demand TLS request",
		zap.String("domain", domain),
		zap.String("client_ip", c.RealIP()),
	)

	if domain == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	if s.config.Server.GatewayDomain != "" && strings.EqualFold(domain, s.config.Server.GatewayDomain) {
		s.logger.Debug("gateway domain auto-allowed for TLS",
			zap.String("domain", domain),
		)
		return c.NoContent(http.StatusOK)
	}

	if net.ParseIP(domain) != nil {
		s.logger.Debug("rejecting IP address in /allowed endpoint",
			zap.String("domain", domain),
		)
		return c.NoContent(http.StatusBadRequest)
	}

	if !isValidDomain(domain) {
		return c.NoContent(http.StatusBadRequest)
	}

	if s.statusCache != nil && s.statusCache.IsDomainActive(domain) {
		s.logger.Debug("status cache confirms domain active, skipping DNS+API",
			zap.String("domain", domain),
		)
		return c.NoContent(http.StatusOK)
	}

	dnsLinkValid := false
	if s.dns != nil {
		dnsLinkPath, err := s.dns.ValidateDNSLink(ctx, domain)
		if err != nil {
			s.logger.Debug("DNSLink validation failed, will attempt portal fallback",
				zap.String("domain", domain),
				zap.Error(err),
			)
		} else if dnsLinkPath == "" {
			s.logger.Debug("DNSLink resolved but path is invalid, will attempt portal fallback",
				zap.String("domain", domain),
			)
		} else {
			dnsLinkValid = true
			s.logger.Debug("DNSLink validation successful",
				zap.String("domain", domain),
				zap.String("dnslink_path", dnsLinkPath),
			)
		}
	} else {
		s.logger.Warn("DNS validator not configured, skipping DNSLink validation")
		dnsLinkValid = true
	}

	if s.api != nil {
		website, err := s.api.GetWebsite(ctx, domain)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				s.logger.Debug("website status check canceled, returning 503",
					zap.String("domain", domain),
					zap.Error(err),
				)
				return c.NoContent(http.StatusServiceUnavailable)
			}
			s.logger.Debug("Website status check failed",
				zap.String("domain", domain),
				zap.Error(err),
			)
			return c.NoContent(http.StatusBadRequest)
		}

		if website.Status != types.StatusActive {
			s.logger.Debug("Website is not active",
				zap.String("domain", domain),
				zap.String("status", string(website.Status)),
			)
			return c.NoContent(http.StatusBadRequest)
		}

		if !dnsLinkValid {
			s.logger.Info("DNSLink validation failed but portal confirms domain is active, allowing for TLS",
				zap.String("domain", domain),
			)
		}

		s.logger.Debug("Website status check successful",
			zap.String("domain", domain),
			zap.String("status", string(website.Status)),
		)
	} else if !dnsLinkValid {
		s.logger.Debug("DNSLink validation failed and no API client to confirm, rejecting",
			zap.String("domain", domain),
		)
		return c.NoContent(http.StatusBadRequest)
	} else {
		s.logger.Warn("API client not configured, skipping website status check")
	}

	s.logger.Info("Domain allowed for certificate issuance",
		zap.String("domain", domain),
	)
	return c.NoContent(http.StatusOK)
}

// isValidDomain performs basic hostname validation according to RFC 1035.
func isValidDomain(domain string) bool {
	if domain == "" {
		return false
	}

	domain = strings.ToLower(domain)

	if len(domain) > 253 {
		return false
	}

	if strings.Contains(domain, "..") {
		return false
	}

	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}

		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}

		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}

	return true
}

// healthCheckHandler handles health check requests.
// Only accessible from loopback addresses (127.0.0.0/8, ::1).
func (s *Server) healthCheckHandler(c *echo.Context) (err error) {
	ctx, span := otel.TraceMethod(c.Request().Context(), "Server.healthCheckHandler")
	defer func() { otel.EndSpanWithErr(span, err) }()

	ip := net.ParseIP(c.RealIP())
	if ip == nil || (!ip.IsLoopback() && !ip.IsUnspecified()) {
		return c.NoContent(http.StatusNotFound)
	}

	if s.healthChecker == nil {
		return c.JSON(http.StatusOK, map[string]string{"status": "up"})
	}

	result := s.healthChecker.Check(ctx)

	if result.Status == health.StatusUp {
		return c.JSON(http.StatusOK, result)
	}
	return c.JSON(http.StatusServiceUnavailable, result)
}


// Start begins serving HTTP requests. Blocks until ctx is cancelled, then gracefully shuts down.
func (s *Server) Start(ctx context.Context, addr string) error {
	sc := echo.StartConfig{
		Address:         addr,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: 30 * time.Second,
	}
	return sc.Start(ctx, s.echo)
}

// DNSValidator defines the interface for DNSLink validation.
type DNSValidator interface {
	ValidateDNSLink(ctx context.Context, domain string) (string, error)
}



// StatusCache defines the interface for caching website status.
type StatusCache interface {
	Get(domain string) *types.CacheResult
	Set(domain string, response *types.GatewayWebsiteResponse)
	SetInvalid(domain string)
	IsDomainActive(domain string) bool
}

// SetHealthChecker sets the health checker for the server.
func (s *Server) SetHealthChecker(checker health.Checker) {
	s.healthChecker = checker
}

// SetDNSValidator sets the DNSLink validator for the server.
func (s *Server) SetDNSValidator(dns DNSValidator) {
	s.dns = dns
}

// SetAPIClient sets the internal API client for the server.
func (s *Server) SetAPIClient(api api.APIClient) {
	s.api = api
}

// SetStatusCache sets the status cache for the server.
func (s *Server) SetStatusCache(cache StatusCache) {
	s.statusCache = cache
}

func (s *Server) SetGateway(g *gw.Gateway) {
	s.gateway = g
}

