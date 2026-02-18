package server

import (
	"context"
	"io"
	"net/http"

	"github.com/alexliesenfeld/health"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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

	e.Use(middleware.Recover())
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `${method} ${uri} ${status} ${remote_ip}` + "\n",
	}))

	srv := &Server{
		echo:   e,
		config: cfg,
		logger: logger,
	}
	srv.setupRoutes(e)
	return srv
}

// setupRoutes initializes HTTP routes for the server.
func (s *Server) setupRoutes(e *echo.Echo) {
	e.GET("/healthz", s.healthCheckHandler)

	e.GET("/ipfs/:cid", func(c echo.Context) error {
		return c.String(http.StatusNotImplemented, "IPFS gateway handler not yet implemented")
	})

	e.GET("/ipfs/:cid/*", func(c echo.Context) error {
		return c.String(http.StatusNotImplemented, "IPFS gateway handler not yet implemented")
	})
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

