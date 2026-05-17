package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/alexliesenfeld/health"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/config"
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
	fetcher      IPFSFetcher
	healthChecker health.Checker
}

// NewServer creates and configures a new Echo server instance.
// It sets up IP extraction for Caddy proxy, adds middleware for recovery,
// logging, and real IP detection, and initializes routes.
func NewServer(cfg *config.Config, logger *zap.Logger) *Server {
	e := echo.New()

	e.HideBanner = true
	e.HidePort = true

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
	e.Use(middleware.Recover())
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `${method} ${uri} ${status} ${remote_ip}` + "\n",
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

	if s.config.RateLimit.Enabled {
		store := middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(s.config.RateLimit.Rate),
				Burst:     s.config.RateLimit.Burst,
				ExpiresIn: s.config.RateLimit.ExpiresIn,
			},
		)

		denyHandler := func(c echo.Context, identifier string, err error) error {
			c.Response().Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.2f", s.config.RateLimit.Rate))
			c.Response().Header().Set("X-RateLimit-Burst", fmt.Sprintf("%d", s.config.RateLimit.Burst))

			s.logger.Warn("rate limit exceeded",
				zap.String("ip", identifier),
			)

			return c.JSON(http.StatusTooManyRequests, map[string]string{
				"error": "rate limit exceeded",
			})
		}

		rateLimitMiddleware := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
			Store:       store,
			DenyHandler: denyHandler,
		})

		e.GET("/allowed", rateLimitMiddleware(s.authMiddleware(s.allowedHandler)))
	} else {
		e.GET("/allowed", s.authMiddleware(s.allowedHandler))
	}

	e.GET("/ipfs/:cid", func(c echo.Context) error {
		return c.String(http.StatusNotImplemented, "IPFS gateway handler not yet implemented")
	})

	e.GET("/ipfs/:cid/*", func(c echo.Context) error {
		return c.String(http.StatusNotImplemented, "IPFS gateway handler not yet implemented")
	})
}

// allowedHandler handles Caddy On-Demand TLS requests to validate domains.
// This endpoint is called by Caddy before issuing SSL certificates.
//
// It validates the domain parameter from the query string and returns:
//   - 200 OK: if the domain is valid and authorized
//   - 400 Bad Request: for all failures (auth, domain validation, DNSLink, API)
//
// SECURITY: Always returns 400 on failure to prevent information leakage.
func (s *Server) allowedHandler(c echo.Context) error {
	ctx := c.Request().Context()

	domain := c.QueryParam("domain")

	s.logger.Debug("Caddy On-Demand TLS request",
		zap.String("domain", domain),
		zap.String("client_ip", c.RealIP()),
	)

	if domain == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	if !isValidDomain(domain) {
		return c.NoContent(http.StatusBadRequest)
	}

	if s.dns != nil {
		dnsLinkPath, err := s.dns.ValidateDNSLink(ctx, domain)
		if err != nil {
			s.logger.Debug("DNSLink validation failed",
				zap.String("domain", domain),
				zap.Error(err),
			)
			return c.NoContent(http.StatusBadRequest)
		}

		if dnsLinkPath == "" {
			s.logger.Debug("DNSLink resolved but path is invalid",
				zap.String("domain", domain),
			)
			return c.NoContent(http.StatusBadRequest)
		}

		s.logger.Debug("DNSLink validation successful",
			zap.String("domain", domain),
			zap.String("dnslink_path", dnsLinkPath),
		)
	} else {
		s.logger.Warn("DNS validator not configured, skipping DNSLink validation")
	}

	if s.api != nil {
		website, err := s.api.GetWebsite(ctx, domain)
		if err != nil {
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

		s.logger.Debug("Website status check successful",
			zap.String("domain", domain),
			zap.String("status", string(website.Status)),
		)
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
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}

	return true
}

// healthCheckHandler handles health check requests.
func (s *Server) healthCheckHandler(c echo.Context) error {
	if s.healthChecker == nil {
		return c.JSON(http.StatusOK, map[string]string{"status": "up"})
	}

	result := s.healthChecker.Check(c.Request().Context())

	if result.Status == health.StatusUp {
		return c.JSON(http.StatusOK, result)
	}
	return c.JSON(http.StatusServiceUnavailable, result)
}


// Start begins serving HTTP requests on the specified address.
func (s *Server) Start(addr string) error {
	s.logger.Info("server starting", zap.String("addr", addr))
	return s.echo.Start(addr)
}

// Shutdown gracefully stops the server, allowing in-flight requests to complete.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("server shutting down")
	return s.echo.Shutdown(ctx)
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
}

// IPFSFetcher defines the interface for fetching content from IPFS.
type IPFSFetcher interface {
	FetchUnixFile(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error)
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

// SetIPFSFetcher sets the IPFS content fetcher for the server.
func (s *Server) SetIPFSFetcher(fetcher IPFSFetcher) {
	s.fetcher = fetcher
}

