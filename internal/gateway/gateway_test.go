package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

type mockAPIClient struct {
	getWebsiteFunc func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
}

func (m *mockAPIClient) GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if m.getWebsiteFunc != nil {
		return m.getWebsiteFunc(ctx, domain)
	}
	return nil, nil
}

func newTestGateway(apiClient api.APIClient, statusCache *cache.StatusCache) (*Gateway, error) {
	return &Gateway{
		logger:      zap.NewNop(),
		api:         apiClient,
		statusCache: statusCache,
	}, nil
}

func TestCheckAccess_ActiveWebsite(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain:     "example.com",
				Status:     types.StatusActive,
				TargetHash: "QmTest",
				TargetType: "ipfs",
			}, nil
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	website, err := gw.CheckAccess(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if website == nil {
		t.Fatal("expected website, got nil")
	}
	if website.Status != types.StatusActive {
		t.Errorf("expected status %s, got %s", types.StatusActive, website.Status)
	}
}

func TestCheckAccess_BrokenWebsite(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain:     "broken.com",
				Status:     types.StatusBroken,
				TargetHash: "QmBroken",
				TargetType: "ipfs",
			}, nil
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	website, err := gw.CheckAccess(context.Background(), "broken.com")
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if website == nil {
		t.Fatal("expected website, got nil")
	}
	if website.Status != types.StatusBroken {
		t.Errorf("expected status %s, got %s", types.StatusBroken, website.Status)
	}
}

func TestCheckAccess_APIDenied(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return nil, errors.New("not found")
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	website, err := gw.CheckAccess(context.Background(), "denied.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if website != nil {
		t.Fatal("expected nil website, got non-nil")
	}
}

func TestCheckAccess_NilAPI(t *testing.T) {
	gw, err := newTestGateway(nil, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	website, err := gw.CheckAccess(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if website != nil {
		t.Fatal("expected nil website with nil API client")
	}
}

func TestCheckAccess_CacheHit(t *testing.T) {
	called := false
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			called = true
			return &types.GatewayWebsiteResponse{
				Domain: "cached.com",
				Status: types.StatusActive,
			}, nil
		},
	}

	statusCache, err := cache.NewStatusCache(100, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewStatusCache: %v", err)
	}

	gw, err := newTestGateway(apiClient, statusCache)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	website, err := gw.CheckAccess(context.Background(), "cached.com")
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if website == nil || website.Status != types.StatusActive {
		t.Fatal("first call should return active website")
	}
	if !called {
		t.Fatal("first call should hit API")
	}

	called = false
	website, err = gw.CheckAccess(context.Background(), "cached.com")
	if err != nil {
		t.Fatalf("CheckAccess cache hit: %v", err)
	}
	if website == nil || website.Status != types.StatusActive {
		t.Fatal("cache hit should return active website")
	}
	if called {
		t.Fatal("cache hit should NOT call API")
	}
}

func TestCheckAccess_CacheInvalid(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return nil, errors.New("not found")
		},
	}

	statusCache, err := cache.NewStatusCache(100, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewStatusCache: %v", err)
	}

	gw, err := newTestGateway(apiClient, statusCache)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	_, err = gw.CheckAccess(context.Background(), "invalid.com")
	if err == nil {
		t.Fatal("expected error for invalid domain")
	}

	result := statusCache.Get("invalid.com")
	if !result.Hit {
		t.Fatal("invalid domain should be cached")
	}
	if result.Entry != nil && result.Entry.Response != nil {
		t.Fatal("cached entry for invalid domain should have nil response")
	}

	website, err := gw.CheckAccess(context.Background(), "invalid.com")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if website != nil {
		t.Fatal("cached invalid domain should return nil website")
	}
}

func TestAccessControlMiddleware_ActiveDomain(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain: "example.com",
				Status: types.StatusActive,
			}, nil
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ipns/example.com/" {
			t.Errorf("expected path /ipns/example.com/, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAccessControlMiddleware_BrokenDomain(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain: "broken.com",
				Status: types.StatusBroken,
			}, nil
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for broken domain")
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "broken.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Errorf("expected 410, got %d", rec.Code)
	}
}

func TestAccessControlMiddleware_DeniedDomain(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return nil, errors.New("not found")
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for denied domain")
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "denied.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAccessControlMiddleware_IPAddressPassThrough(t *testing.T) {
	gw, err := newTestGateway(nil, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("IP address should pass through to inner handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAccessControlMiddleware_IpfSPrefixURLPassThrough(t *testing.T) {
	gw, err := newTestGateway(nil, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/ipfs/QmTest", nil)
	req.Host = "gateway.example.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("/ipfs/ path should pass through to inner handler")
	}
}

func TestAccessControlMiddleware_XForwardedHost(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			if domain != "behind-proxy.com" {
				t.Errorf("expected domain behind-proxy.com, got %s", domain)
			}
			return &types.GatewayWebsiteResponse{
				Domain: "behind-proxy.com",
				Status: types.StatusActive,
			}, nil
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Host", "behind-proxy.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAccessControlMiddleware_PathRewrite(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain: "example.com",
				Status: types.StatusActive,
			}, nil
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	var receivedPath string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/assets/style.css", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if receivedPath != "/ipns/example.com/assets/style.css" {
		t.Errorf("expected /ipns/example.com/assets/style.css, got %s", receivedPath)
	}
}

func TestAccessControlMiddleware_PendingValidationStatus(t *testing.T) {
	apiClient := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain: "pending.com",
				Status: types.StatusPendingValidation,
			}, nil
		},
	}

	gw, err := newTestGateway(apiClient, nil)
	if err != nil {
		t.Fatalf("newTestGateway: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for pending domain")
	})

	middleware := NewAccessControlMiddleware(gw, zap.NewNop())
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "pending.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for pending_validation, got %d", rec.Code)
	}
}

func TestStripPort(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com:8080", "example.com"},
		{"example.com", "example.com"},
		{"[::1]:8080", "::1"},
	}

	for _, tt := range tests {
		result := stripPort(tt.input)
		if result != tt.expected {
			t.Errorf("stripPort(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
