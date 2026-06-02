package gateway

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	gopath "path"
	"strings"
	"time"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/gateway"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	"github.com/libp2p/go-libp2p/core/routing"
	routinghelpers "github.com/libp2p/go-libp2p-routing-helpers"
	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	ipfs "go.lumeweb.com/ipfs-sdk"
	stalenamesys "go.lumeweb.com/ipfs-website-gateway/internal/namesys"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

//go:embed templates/base.html
var baseTemplate string

//go:embed templates/pending_validation.html
var pendingTemplate string

//go:embed templates/invalid_site.html
var invalidTemplate string

type ErrorPageData struct {
	Title           string
	Domain          string
	StatusText      string
	Explanation     string
	Reasons         string
	ContentTemplate string
}

type Gateway struct {
	backend     *gateway.BlocksBackend
	handler     http.Handler
	logger      *zap.Logger
	api         api.APIClient
	statusCache *cache.StatusCache
	nameSys     *stalenamesys.StaleWhileRevalidateNameSystem
}

func NewGateway(bs blockservice.BlockService, apiClient api.APIClient, statusCache *cache.StatusCache, logger *zap.Logger, retrievalTimeout time.Duration, valueStore routing.ValueStore, ipnsCacheSize int, ipnsFreshTTL time.Duration, ipnsCachePath string, pubsubEnabled bool) (*Gateway, error) {
	ns, err := gateway.NewDNSResolver(nil, nil)
	if err != nil {
		return nil, err
	}

	if valueStore == nil {
		valueStore = routinghelpers.Null{}
	}

	nsLogger := logger.Named("namesys")
	nsLogger.Debug("valueStore initialized",
		zap.String("type", fmt.Sprintf("%T", valueStore)),
	)

	ipnsStore, err := stalenamesys.NewIPNSStore(ipnsCachePath, ipnsFreshTTL, retrievalTimeout, ipnsCacheSize, nsLogger)
	if err != nil {
		return nil, err
	}

	namesysOpts := []namesys.Option{
		namesys.WithDNSResolver(ns),
		namesys.WithDatastore(ipnsStore.Datastore()),
	}
	// NOTE: Boxo's internal LRU cache (WithCache/WithMaxCacheTTL) is intentionally
	// omitted. StaleWhileRevalidateNameSystem provides its own caching layer via
	// IPNSStore with stale-while-revalidate semantics. A separate Boxo LRU cache
	// creates a shadow copy that can overwrite fresh pubsub updates during
	// revalidation, causing 15-30s propagation delays.

	nameSystem, err := namesys.NewNameSystem(valueStore, namesysOpts...)
	if err != nil {
		_ = ipnsStore.Close()
		return nil, err
	}

	nsLogger.Debug("boxo namesys created",
		zap.String("valueStore_type", fmt.Sprintf("%T", valueStore)),
		zap.Int("ipns_store_lru_size", ipnsCacheSize),
		zap.Duration("ipns_store_fresh_ttl", ipnsFreshTTL),
		zap.Bool("pubsub_enabled", pubsubEnabled),
	)

	wrappedNameSystem := stalenamesys.NewStaleWhileRevalidateNameSystem(nameSystem, ipnsStore, 4, nsLogger)
	if pubsubEnabled {
		wrappedNameSystem.EnableWatch()
		wrappedNameSystem.WarmSubscriptions()
	}

	backend, err := gateway.NewBlocksBackend(bs, gateway.WithNameSystem(wrappedNameSystem))
	if err != nil {
		wrappedNameSystem.Stop()
		return nil, err
	}

	cfg := gateway.Config{
		NoDNSLink:             true,
		DeserializedResponses: true,
		PublicGateways:        map[string]*gateway.PublicGateway{},
		RetrievalTimeout:      retrievalTimeout,
	}

	handler := gateway.NewHandler(cfg, backend)

	return &Gateway{
		backend:     backend,
		handler:     handler,
		logger:      logger,
		api:         apiClient,
		statusCache: statusCache,
		nameSys:     wrappedNameSystem,
	}, nil
}

func (g *Gateway) Close() {
	if g.nameSys != nil {
		g.nameSys.Stop()
	}
}

func (g *Gateway) SetPrewarmCallback(fn stalenamesys.PrewarmCallback) {
	g.nameSys.SetPrewarmCallback(fn)
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
		if result.Hit && !result.Expired && result.Entry != nil {
			g.logger.Debug("status cache hit",
				zap.String("domain", domain),
			)
			if result.Entry.Err != nil {
				return nil, result.Entry.Err
			}
			return result.Entry.Response, nil
		}
		if result.Hit && result.Expired {
			g.logger.Debug("status cache expired, revalidating",
				zap.String("domain", domain),
			)
		}
		if !result.Hit {
			g.logger.Debug("status cache miss",
				zap.String("domain", domain),
			)
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
			if errors.Is(err, ipfs.ErrNotFound) {
				// Domain genuinely doesn't exist on the platform — safe to cache longer
				g.statusCache.SetError(domain, err)
			} else {
				// Everything else (network errors, 500s, auth issues, gone) may be transient
				g.statusCache.SetErrorShortTTL(domain, err)
			}
		}
		return nil, err
	}

	if g.statusCache != nil {
		if website != nil && website.Status == types.StatusPendingValidation {
			g.statusCache.SetShortTTL(domain, website)
		} else if website != nil && website.Status == types.StatusBroken {
			g.statusCache.SetShortTTL(domain, website)
		} else {
			g.statusCache.Set(domain, website)
		}
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
	gateway   *Gateway
	logger    *zap.Logger
	templates *template.Template
}

func NewAccessControlMiddleware(gw *Gateway, logger *zap.Logger) (*AccessControlMiddleware, error) {
	tmpl, err := template.New("base").Parse(baseTemplate + pendingTemplate + invalidTemplate)
	if err != nil {
		return nil, err
	}

	return &AccessControlMiddleware{
		gateway:   gw,
		logger:    logger,
		templates: tmpl,
	}, nil
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
			if errors.Is(err, ipfs.ErrNotFound) {
				m.logger.Debug("domain not found on platform",
					zap.String("domain", domain),
					zap.Error(err),
				)
				m.renderInvalidPage(w, http.StatusNotFound, domain, "Not On Platform",
					"This website is not hosted on Pinner.",
					"This domain has not been registered on the Pinner network. If you believe this is an error, please check your DNS configuration or register your domain at pinner.xyz.")
				return
			}

			if errors.Is(err, ipfs.ErrGone) {
				m.logger.Debug("domain gone",
					zap.String("domain", domain),
					zap.Error(err),
				)
				m.renderInvalidPage(w, http.StatusGone, domain, "Website Unavailable",
					"This website has been marked as broken or removed from the Pinner network.",
					"The content may be inaccessible, the target hash may be invalid, or the website may have been taken down by the owner.")
				return
			}

			if errors.Is(err, ipfs.ErrUnauthorized) {
				m.logger.Debug("access denied for domain",
					zap.String("domain", domain),
					zap.Error(err),
				)
				m.renderInvalidPage(w, http.StatusNotFound, domain, "Access Error",
					"Unable to verify this domain at this time.",
					"We're experiencing technical difficulties checking this domain's status. Please try again later or contact support if the problem persists.")
				return
			}

			m.logger.Debug("access check failed for domain",
				zap.String("domain", domain),
				zap.Error(err),
			)
			m.renderInvalidPage(w, http.StatusNotFound, domain, "Access Error",
				"Unable to verify this domain at this time.",
				"We're experiencing technical difficulties checking this domain's status. Please try again later or contact support if the problem persists.")
			return
		}

		if website == nil {
			m.logger.Debug("domain not found", zap.String("domain", domain))
			m.renderInvalidPage(w, http.StatusNotFound, domain, "Not On Platform",
				"This website is not hosted on Pinner.",
				"This domain has not been registered on the Pinner network. If you believe this is an error, please check your DNS configuration or register your domain at pinner.xyz.")
			return
		}

		if website.Status == types.StatusBroken {
			m.logger.Debug("domain broken", zap.String("domain", domain))
			m.renderInvalidPage(w, http.StatusGone, domain, "Website Unavailable",
				"This website has been marked as broken or removed from the Pinner network.",
				"The content may be inaccessible, the target hash may be invalid, or the website may have been taken down by the owner.")
			return
		}

		if website.Status == types.StatusPendingValidation {
			m.logger.Debug("domain pending validation",
				zap.String("domain", domain),
				zap.String("status", string(website.Status)),
			)
			m.renderPendingPage(w, domain)
			return
		}

		if website.Status != types.StatusActive {
			m.logger.Debug("domain not active",
				zap.String("domain", domain),
				zap.String("status", string(website.Status)),
			)
			m.renderInvalidPage(w, http.StatusNotFound, domain, "Website Inactive",
				"This website is currently inactive.",
				"The website may be awaiting validation, suspended, or temporarily disabled.")
			return
		}

		originalPath := r.URL.Path
		switch website.TargetType {
		case "ipfs":
			r.URL.Path = "/ipfs/" + website.TargetHash + r.URL.Path
		case "ipns":
			r.URL.Path = "/ipns/" + website.TargetHash + r.URL.Path
		default:
			m.logger.Error("unknown target type",
				zap.String("domain", domain),
				zap.String("target_type", website.TargetType),
			)
			m.renderInvalidPage(w, http.StatusNotFound, domain, "Configuration Error",
				"This website has an invalid configuration.",
				"The target type is not recognized. Please contact support.")
			return
		}
		ctx = context.WithValue(ctx, gateway.DNSLinkHostnameKey, domain)
		r = r.WithContext(ctx)

		m.logger.Debug("rewriting path for active domain",
			zap.String("domain", domain),
			zap.String("target_type", website.TargetType),
			zap.String("original_path", originalPath),
			zap.String("rewritten_path", r.URL.Path),
		)
		next.ServeHTTP(newSubResourceErrorHandler(w, r), r)
	})
}

func stripPort(hostname string) string {
	host, _, err := net.SplitHostPort(hostname)
	if err == nil {
		return host
	}
	return hostname
}

func (m *AccessControlMiddleware) renderPendingPage(w http.ResponseWriter, domain string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := ErrorPageData{
		Title:           "Awaiting Validation",
		Domain:          domain,
		ContentTemplate: "pending_content",
	}

	var buf bytes.Buffer
	if err := m.templates.ExecuteTemplate(&buf, "base", data); err != nil {
		m.logger.Error("failed to render pending template", zap.Error(err))
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}

func (m *AccessControlMiddleware) renderInvalidPage(w http.ResponseWriter, statusCode int, domain, statusText, explanation, reasons string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := ErrorPageData{
		Title:           "Site Unavailable",
		Domain:          domain,
		StatusText:      statusText,
		Explanation:     explanation,
		Reasons:         reasons,
		ContentTemplate: "invalid_content",
	}

	var buf bytes.Buffer
	if err := m.templates.ExecuteTemplate(&buf, "base", data); err != nil {
		m.logger.Error("failed to render invalid template", zap.Error(err))
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(statusCode)
	_, _ = buf.WriteTo(w)
}

// subResourceErrorHandler wraps http.ResponseWriter to intercept error
// responses for sub-resource requests (CSS, JS, images, etc). When Boxo
// fails to fetch a block, it returns text/plain error bodies that cause
// browsers to report misleading MIME type errors for sub-resources. This
// wrapper suppresses the error body for sub-resource requests, returning
// only the status code so the browser shows a clean network error instead.
type subResourceErrorHandler struct {
	http.ResponseWriter
	request    *http.Request
	statusCode int
	wroteHeader bool
	bodyBuf    bytes.Buffer
}

func newSubResourceErrorHandler(w http.ResponseWriter, r *http.Request) *subResourceErrorHandler {
	return &subResourceErrorHandler{
		ResponseWriter: w,
		request:        r,
	}
}

func (s *subResourceErrorHandler) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.statusCode = code
	s.wroteHeader = true

	if isSubResourceRequest(s.request) && code >= 400 {
		s.Header().Del("Content-Length")
		s.Header().Del("Content-Type")
	}

	s.ResponseWriter.WriteHeader(code)
}

func (s *subResourceErrorHandler) Write(p []byte) (int, error) {
	if !s.wroteHeader {
		s.WriteHeader(http.StatusOK)
	}

	if isSubResourceRequest(s.request) && s.statusCode >= 400 {
		return len(p), nil
	}

	return s.ResponseWriter.Write(p)
}

func (s *subResourceErrorHandler) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func isSubResourceRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		return false
	}

	ext := strings.ToLower(gopath.Ext(r.URL.Path))
	switch ext {
	case ".css", ".js", ".mjs", ".woff", ".woff2", ".ttf",
		".eot", ".otf", ".png", ".jpg", ".jpeg", ".gif",
		".svg", ".ico", ".webp", ".avif", ".webm", ".mp4",
		".wasm", ".json", ".xml":
		return true
	}
	return false
}
