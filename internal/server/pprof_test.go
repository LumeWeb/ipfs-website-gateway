package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"go.lumeweb.com/ipfs-website-gateway/internal/config"
	"go.uber.org/zap"
)

// testGatewaySecret returns a fresh random token to use as the pprof auth
// secret in tests. It is generated at runtime so no secret-shaped literal
// lives in source.
func testGatewaySecret(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("failed to generate test secret: %v", err)
	}
	return hex.EncodeToString(buf)
}

func newPprofTestServer(t *testing.T, allowedSecret string, pprofEnabled bool) *Server {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:          8080,
			AllowedSecret: allowedSecret,
		},
		Observability: config.ObservabilityConfig{
			Enabled: pprofEnabled,
			Pprof: config.PprofConfig{
				Enabled: pprofEnabled,
				Path:    "/debug/pprof",
			},
		},
	}
	s := NewServer(cfg, zap.NewNop())
	s.InitializeRoutes()
	return s
}

func TestPprof_RequiresGatewaySecretHeader(t *testing.T) {
	secret := testGatewaySecret(t)
	s := newPprofTestServer(t, secret, true)

	tests := []struct {
		name           string
		secret         string
		expectedStatus int
	}{
		{"correct secret returns 200", secret, http.StatusOK},
		{"missing secret returns 401", "", http.StatusUnauthorized},
		{"wrong secret returns 401", "wrong", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
			if tt.secret != "" {
				req.Header.Set(gatewaySecretHeader, tt.secret)
			}
			rec := httptest.NewRecorder()
			s.echo.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestPprof_FailsClosedWhenNoSecretConfigured(t *testing.T) {
	// Pprof exposes sensitive in-memory state, so it fails closed when no
	// secret is configured (unlike /allowed). A request without the header
	// must be rejected with 401.
	s := newPprofTestServer(t, "", true)

	tests := []struct {
		name string
		path string
	}{
		{"index", "/debug/pprof/"},
		{"profile", "/debug/pprof/profile"},
		{"heap", "/debug/pprof/heap"},
		{"goroutine", "/debug/pprof/goroutine"},
		{"cmdline", "/debug/pprof/cmdline"},
	}

	for _, ts := range tests {
		t.Run(ts.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ts.path, nil)
			rec := httptest.NewRecorder()
			s.echo.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for %s when no secret, got %d", ts.path, rec.Code)
			}
		})
	}
}

func TestPprof_DisabledWhenNotEnabled(t *testing.T) {
	// Pprof disabled -> routes are not registered -> 404.
	secret := testGatewaySecret(t)
	s := newPprofTestServer(t, secret, false)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil)
	req.Header.Set(gatewaySecretHeader, secret)
	rec := httptest.NewRecorder()
	s.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when pprof disabled, got %d", rec.Code)
	}
}

func TestPprof_IndexServedAtBaseAndTrailingSlash(t *testing.T) {
	secret := testGatewaySecret(t)
	s := newPprofTestServer(t, secret, true)

	// Trailing-slash route serves the index directly.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set(gatewaySecretHeader, secret)
	rec := httptest.NewRecorder()
	s.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /debug/pprof/, got %d", rec.Code)
	}

	// Bare route redirects to the trailing-slash index.
	req = httptest.NewRequest(http.MethodGet, "/debug/pprof", nil)
	req.Header.Set(gatewaySecretHeader, secret)
	rec = httptest.NewRecorder()
	s.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("expected 301 redirect for /debug/pprof, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/debug/pprof/" {
		t.Errorf("expected redirect Location /debug/pprof/, got %q", loc)
	}
}

func TestPprof_BarePathFailsClosedWithoutSecret(t *testing.T) {
	// The bare /debug/pprof redirect route is auth-gated like every other
	// pprof endpoint: a request without a valid secret must get 401, not a
	// redirect that reveals pprof is enabled.
	secret := testGatewaySecret(t)
	s := newPprofTestServer(t, secret, true)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof", nil)
	rec := httptest.NewRecorder()
	s.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for bare path without secret, got %d", rec.Code)
	}
}

func TestPprof_ProfileSecondsClamped(t *testing.T) {
	// ?seconds= on /profile and /trace must be clamped to maxProfileSeconds so
	// an authorized caller cannot force an unbounded CPU block.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/profile?seconds=9999", nil)
	clampSecondsHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("seconds"); got != strconv.Itoa(maxProfileSeconds) {
			t.Errorf("expected seconds clamped to %d, got %q", maxProfileSeconds, got)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
}

func TestPprof_SingleFlightRejectsConcurrent(t *testing.T) {
	// A concurrent CPU profile/trace request must be rejected with 409 while
	// another is in flight, since Go serializes CPU profiling globally.
	profileFlight.Lock()
	defer profileFlight.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/profile", nil)
	rec := httptest.NewRecorder()
	blocked := make(chan bool, 1)
	singleFlightHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blocked <- true
	})).ServeHTTP(rec, req)

	select {
	case <-blocked:
		t.Fatal("handler ran while another profile was in flight; expected 409 rejection")
	default:
	}

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for concurrent profile, got %d", rec.Code)
	}
}

func TestPprof_SingleFlightAllowsWhenFree(t *testing.T) {
	// When no profile is in flight, the handler runs normally.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/profile", nil)
	rec := httptest.NewRecorder()
	ran := false
	singleFlightHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
	})).ServeHTTP(rec, req)

	if !ran {
		t.Fatal("handler did not run when no profile was in flight")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 when profile free, got %d", rec.Code)
	}
}

func TestPprof_BarePathRedirectNotRegisteredWithoutAuth(t *testing.T) {
	// Sanity: with auth failing closed, the bare redirect is 401 not 301.
	secret := testGatewaySecret(t)
	s := newPprofTestServer(t, secret, true)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof", nil)
	req.Header.Set(gatewaySecretHeader, "not-the-secret")
	rec := httptest.NewRecorder()
	s.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for bare path with wrong secret, got %d", rec.Code)
	}
}

func TestPprof_SubprofilesResolve(t *testing.T) {
	secret := testGatewaySecret(t)
	s := newPprofTestServer(t, secret, true)

	for _, path := range []string{
		"/debug/pprof/goroutine",
		"/debug/pprof/heap",
		"/debug/pprof/block",
		"/debug/pprof/mutex",
		"/debug/pprof/allocs",
		"/debug/pprof/threadcreate",
		"/debug/pprof/cmdline",
		"/debug/pprof/symbol",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set(gatewaySecretHeader, secret)
			rec := httptest.NewRecorder()
			s.echo.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 for %s, got %d", path, rec.Code)
			}
		})
	}
}

func TestPprof_RejectsNonDefaultPath(t *testing.T) {
	// net/http/pprof hardcodes /debug/pprof, so any other configured path is
	// rejected at registration time rather than silently serving a broken
	// index page.
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Observability: config.ObservabilityConfig{
			Enabled: true,
			Pprof:   config.PprofConfig{Enabled: true, Path: "/debug/mem"},
		},
	}
	s := NewServer(cfg, zap.NewNop())

	err := s.registerPprofRoutes(s.echo, "/debug/mem")
	if err == nil {
		t.Fatal("expected error for non-default pprof path, got nil")
	}
}

func TestPprof_BadPathDegradesToDisabled(t *testing.T) {
	// A non-default pprof path must not abort startup; pprof simply stays
	// unregistered (404) and the rest of the server still runs.
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Observability: config.ObservabilityConfig{
			Enabled: true,
			Pprof:   config.PprofConfig{Enabled: true, Path: "/debug/mem"},
		},
	}
	s := NewServer(cfg, zap.NewNop())

	// Should not panic / exit; InitializeRoutes registers routes internally.
	s.InitializeRoutes()

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	s.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for bad-path-disabled pprof, got %d", rec.Code)
	}
}
