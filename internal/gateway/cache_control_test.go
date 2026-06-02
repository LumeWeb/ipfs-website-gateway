package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCacheControlMiddleware_RewritesMutableIPNSPath(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Etag", `"test-etag"`)
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewCacheControlMiddleware()
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/ipns/example.com/index.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=0, must-revalidate" {
		t.Errorf("expected Cache-Control 'public, max-age=0, must-revalidate', got %q", cc)
	}
	if etag := rec.Header().Get("Etag"); etag != `"test-etag"` {
		t.Errorf("expected ETag to be preserved, got %q", etag)
	}
}

func TestCacheControlMiddleware_RewritesImmutableIPFSPath(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=29030400, immutable")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewCacheControlMiddleware()
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/ipfs/bafybeihqjmf3b7z2zkencefihq5bk4g2ia2x2l222f6imoxsnfp7serrsu/index.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=0, must-revalidate" {
		t.Errorf("expected Cache-Control rewritten for /ipfs/ website path, got %q", cc)
	}
}

func TestCacheControlMiddleware_RewritesIPFSAssetPath(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=29030400, immutable")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewCacheControlMiddleware()
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/ipfs/bafybeihqjmf3b7z2zkencefihq5bk4g2ia2x2l222f6imoxsnfp7serrsu/assets/style.css", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=0, must-revalidate" {
		t.Errorf("expected Cache-Control rewritten for /ipfs/ asset path, got %q", cc)
	}
}

func TestCacheControlMiddleware_IPNSImplicitWriteHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("hello"))
	})

	middleware := NewCacheControlMiddleware()
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/ipns/example.com/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=0, must-revalidate" {
		t.Errorf("expected Cache-Control rewritten on implicit WriteHeader, got %q", cc)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("expected body 'hello', got %q", rec.Body.String())
	}
}

func TestCacheControlMiddleware_MultiWriteHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusNotFound)
	})

	middleware := NewCacheControlMiddleware()
	handler := middleware.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/ipns/example.com/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected first WriteHeader to win (200), got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=0, must-revalidate" {
		t.Errorf("expected Cache-Control rewritten, got %q", cc)
	}
}
