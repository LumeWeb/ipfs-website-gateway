package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHardcodedSecurityHeaders(t *testing.T) {
	assert.NotEmpty(t, HardcodedSecurityHeaders)
	assert.Equal(t, "nosniff", HardcodedSecurityHeaders["X-Content-Type-Options"])
	assert.Equal(t, "strict-origin-when-cross-origin", HardcodedSecurityHeaders["Referrer-Policy"])
}

func TestHeadersFilename(t *testing.T) {
	assert.Equal(t, "_headers", HeadersFilename)
}

func TestIsHeaderAllowed(t *testing.T) {
	h := NewHeadersMiddleware(zap.NewNop())

	tests := []struct {
		name     string
		header   string
		expected bool
	}{
		{"CSP allowed", "content-security-policy", true},
		{"X-Frame-Options allowed", "x-frame-options", true},
		{"Custom X- header", "x-custom-header", true},
		{"Random header", "random", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := h.isHeaderAllowed(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchPath(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		matches bool
	}{
		{"exact match", "/blog", "/blog", true},
		{"wildcard root", "/*", "/anything", true},
		{"wildcard prefix", "/blog/*", "/blog/post-1", true},
		{"wildcard prefix no match", "/blog/*", "/other", false},
		{"glob pattern", "/blog/*.html", "/blog/post.html", true},
		{"glob no match", "/blog/*.html", "/blog/post.txt", false},
		{"root", "/", "/", true},
		{"no match", "/admin", "/user", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPath(tt.pattern, tt.path)
			assert.Equal(t, tt.matches, result)
		})
	}
}

func TestHeadersMiddleware_Parse(t *testing.T) {
	tests := []struct {
		name            string
		headersContent  string
		requestPath     string
		expectedHeaders map[string]string
		unexpectedKeys  []string
	}{
		{
			name:           "applies custom headers",
			headersContent: "/\n  X-Frame-Options: DENY\n  Content-Security-Policy: default-src 'self'",
			requestPath:    "/",
			expectedHeaders: map[string]string{
				"X-Frame-Options":         "DENY",
				"Content-Security-Policy": "default-src 'self'",
			},
		},
		{
			name:           "blocks operator-only headers",
			headersContent: "/\n  Strict-Transport-Security: max-age=31536000",
			requestPath:    "/",
			unexpectedKeys: []string{"Strict-Transport-Security"},
		},
		{
			name:           "user cannot override hardcoded",
			headersContent: "/\n  X-Content-Type-Options: sniff",
			requestPath:    "/",
			expectedHeaders: map[string]string{
				"X-Content-Type-Options": "nosniff",
			},
		},
		{
			name:           "path-specific rules",
			headersContent: "/*\n  X-Frame-Options: DENY\n/admin/*\n  X-Frame-Options: SAMEORIGIN",
			requestPath:    "/admin/dashboard",
			expectedHeaders: map[string]string{
				"X-Frame-Options": "SAMEORIGIN",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHeadersMiddleware(zap.NewNop())
			err := h.Parse(strings.NewReader(tt.headersContent))
			assert.NoError(t, err)

			rec := httptest.NewRecorder()
			h.ApplyHeaders(rec, tt.requestPath)

			ApplyDefaultSecurityHeaders(rec)

			for key, expected := range tt.expectedHeaders {
				assert.Equal(t, expected, rec.Header().Get(key), "header %s mismatch", key)
			}
			for _, key := range tt.unexpectedKeys {
				assert.Empty(t, rec.Header().Get(key), "header %s should not be set", key)
			}
		})
	}
}

func TestApplyDefaultSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	ApplyDefaultSecurityHeaders(rec)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
	assert.Equal(t, "noopen", rec.Header().Get("X-Download-Options"))
	assert.Equal(t, "none", rec.Header().Get("X-Permitted-Cross-Domain-Policies"))
}

func TestValidateHeadersFile(t *testing.T) {
	content := "/\n  Content-Security-Policy: default-src 'self'\n  Strict-Transport-Security: max-age=31536000\n  X-Custom-Header: value"

	warnings := ValidateHeadersFile(content)

	foundHSTSWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "Strict-Transport-Security") {
			foundHSTSWarning = true
		}
	}
	assert.True(t, foundHSTSWarning, "should warn about HSTS")
}

func TestHeadersMiddleware_HasRules(t *testing.T) {
	h := NewHeadersMiddleware(zap.NewNop())
	assert.False(t, h.HasRules())

	err := h.Parse(strings.NewReader("/\n  X-Frame-Options: DENY"))
	assert.NoError(t, err)
	assert.True(t, h.HasRules())
}
