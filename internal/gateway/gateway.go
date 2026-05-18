package gateway

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/gateway"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	routinghelpers "github.com/libp2p/go-libp2p-routing-helpers"
	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

type Gateway struct {
	backend     *gateway.BlocksBackend
	handler     http.Handler
	logger      *zap.Logger
	api         api.APIClient
	statusCache *cache.StatusCache
}

func NewGateway(bs blockservice.BlockService, apiClient api.APIClient, statusCache *cache.StatusCache, logger *zap.Logger) (*Gateway, error) {
	ns, err := gateway.NewDNSResolver(nil, nil)
	if err != nil {
		return nil, err
	}

	nameSystem, err := namesys.NewNameSystem(routinghelpers.Null{}, namesys.WithDNSResolver(ns))
	if err != nil {
		return nil, err
	}

	backend, err := gateway.NewBlocksBackend(bs, gateway.WithNameSystem(nameSystem))
	if err != nil {
		return nil, err
	}

	cfg := gateway.Config{
		NoDNSLink:             true,
		DeserializedResponses: true,
		PublicGateways:        map[string]*gateway.PublicGateway{},
		RetrievalTimeout:      30 * time.Second,
	}

	handler := gateway.NewHandler(cfg, backend)

	return &Gateway{
		backend:     backend,
		handler:     handler,
		logger:      logger,
		api:         apiClient,
		statusCache: statusCache,
	}, nil
}

func (g *Gateway) Handler() http.Handler {
	return g.handler
}

func (g *Gateway) Backend() *gateway.BlocksBackend {
	return g.backend
}

func (g *Gateway) CheckAccess(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if g.statusCache != nil {
		result := g.statusCache.Get(domain)
		if result.Hit && !result.Expired && result.Entry != nil && result.Entry.Response != nil {
			return result.Entry.Response, nil
		}
		if result.Hit && !result.Expired && result.Entry != nil && result.Entry.Response == nil {
			return nil, nil
		}
	}

	if g.api == nil {
		return nil, nil
	}

	website, err := g.api.GetWebsite(ctx, domain)
	if err != nil {
		g.logger.Debug("website status check failed",
			zap.String("domain", domain),
			zap.Error(err),
		)
		if g.statusCache != nil {
			g.statusCache.SetInvalid(domain)
		}
		return nil, err
	}

	if g.statusCache != nil {
		g.statusCache.Set(domain, website)
	}

	return website, nil
}

func (g *Gateway) GetDNSLinkRecord(ctx context.Context, hostname string) (path.Path, error) {
	return g.backend.GetDNSLinkRecord(ctx, hostname)
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}

type AccessControlMiddleware struct {
	gateway *Gateway
	logger  *zap.Logger
}

func NewAccessControlMiddleware(gw *Gateway, logger *zap.Logger) *AccessControlMiddleware {
	return &AccessControlMiddleware{gateway: gw, logger: logger}
}

func (m *AccessControlMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		host := r.Host
		if xHost := r.Header.Get("X-Forwarded-Host"); xHost != "" {
			host = xHost
		}

		domain := stripPort(host)

		m.logger.Debug("incoming request",
			zap.String("host", r.Host),
			zap.String("x-forwarded-host", r.Header.Get("X-Forwarded-Host")),
			zap.String("domain", domain),
			zap.String("path", r.URL.Path),
			zap.String("method", r.Method),
		)

		if net.ParseIP(domain) != nil {
			m.logger.Debug("passing through IP address", zap.String("domain", domain))
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/ipfs/") || strings.HasPrefix(r.URL.Path, "/ipns/") {
			m.logger.Debug("passing through ipfs/ipns path", zap.String("path", r.URL.Path))
			next.ServeHTTP(w, r)
			return
		}

		website, err := m.gateway.CheckAccess(ctx, domain)
		if err != nil {
			m.logger.Debug("access denied for domain",
				zap.String("domain", domain),
				zap.Error(err),
			)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if website == nil {
			m.logger.Debug("domain not found", zap.String("domain", domain))
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if website.Status == types.StatusBroken {
			m.logger.Debug("domain broken", zap.String("domain", domain))
			http.Error(w, "gone", http.StatusGone)
			return
		}

		if website.Status != types.StatusActive {
			m.logger.Debug("domain not active",
				zap.String("domain", domain),
				zap.String("status", string(website.Status)),
			)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		originalPath := r.URL.Path
		r.URL.Path = "/ipns/" + domain + r.URL.Path
		m.logger.Debug("rewriting path for active domain",
			zap.String("domain", domain),
			zap.String("original_path", originalPath),
			zap.String("rewritten_path", r.URL.Path),
		)
		next.ServeHTTP(w, r)
	})
}

func stripPort(hostname string) string {
	host, _, err := net.SplitHostPort(hostname)
	if err == nil {
		return host
	}
	return hostname
}
