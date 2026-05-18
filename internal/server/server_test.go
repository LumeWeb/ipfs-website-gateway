package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.lumeweb.com/ipfs-website-gateway/internal/config"
	"go.uber.org/zap"
)

func TestNewServer(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8"},
		},
	}

	server := NewServer(cfg, logger)
	server.InitializeRoutes()

	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.echo == nil {
		t.Error("Server echo field is nil")
	}
	if server.config != cfg {
		t.Error("Server config field is not set correctly")
	}
	if server.logger != logger {
		t.Error("Server logger field is not set correctly")
	}
}

func TestServerRoutes(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)
	server.InitializeRoutes()

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "health check",
			method:         http.MethodGet,
			path:           "/healthz",
			expectedStatus: http.StatusOK,
			expectedBody:   "\"status\"",
		},
		{
			name:           "ipfs cid without path",
			method:         http.MethodGet,
			path:           "/ipfs/QmTest",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "ipfs cid with path",
			method:         http.MethodGet,
			path:           "/ipfs/QmTest/some/path/file.txt",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			server.echo.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("Expected body to contain %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
}

func TestServerMiddleware(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{"127.0.0.1"},
		},
	}

	server := NewServer(cfg, logger)
	server.InitializeRoutes()

	t.Run("recover middleware handles panic", func(t *testing.T) {
		server.echo.GET("/panic", func(c echo.Context) error {
			panic("test panic")
		})

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()
		server.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500 from panic recovery, got %d", rec.Code)
		}
	})

	t.Run("IP extractor extracts from X-Real-IP header", func(t *testing.T) {
		logger := zap.NewNop()
		cfgWithProxies := &config.Config{
			Server: config.ServerConfig{
				Port:           8080,
				TrustedProxies: []string{"127.0.0.1"},
			},
		}
		serverWithProxies := NewServer(cfgWithProxies, logger)
		serverWithProxies.InitializeRoutes()

		serverWithProxies.echo.GET("/ip", func(c echo.Context) error {
			// Check if IP extractor set the real IP
			if realIP := c.RealIP(); realIP != "" {
				return c.String(http.StatusOK, realIP)
			}
			return c.String(http.StatusNotFound, "no real_ip set")
		})

		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Real-IP", "10.0.0.1")
		rec := httptest.NewRecorder()
		serverWithProxies.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if body != "10.0.0.1" {
			t.Errorf("Expected RealIP to be '10.0.0.1' from X-Real-IP, got '%s'", body)
		}
	})

	t.Run("IP extractor extracts from X-Forwarded-For header", func(t *testing.T) {
		logger := zap.NewNop()
		cfgWithProxies := &config.Config{
			Server: config.ServerConfig{
				Port:           8080,
				TrustedProxies: []string{"127.0.0.1"},
			},
		}
		serverWithProxies := NewServer(cfgWithProxies, logger)
		serverWithProxies.InitializeRoutes()

		serverWithProxies.echo.GET("/ip", func(c echo.Context) error {
			// Check if IP extractor set the real IP
			if realIP := c.RealIP(); realIP != "" {
				return c.String(http.StatusOK, realIP)
			}
			return c.String(http.StatusNotFound, "no real_ip set")
		})

		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		// Use public IP in XFF to ensure it's untrusted (private nets are trusted)
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.2, 10.0.0.3")
		rec := httptest.NewRecorder()
		serverWithProxies.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		// ExtractIPFromXFFHeader returns the nearest untrustable IP.
		// Since 127.0.0.1 is trusted (loopback), and 10.0.0.2, 10.0.0.3 are private (trusted),
		// it should return 203.0.113.1 (the untrusted public IP).
		if body != "203.0.113.1" {
			t.Errorf("Expected RealIP to be '203.0.113.1' from X-Forwarded-For, got '%s'", body)
		}
	})

	t.Run("IP extractor falls back to RemoteAddr when headers are missing", func(t *testing.T) {
		logger := zap.NewNop()
		cfgWithProxies := &config.Config{
			Server: config.ServerConfig{
				Port:           8080,
				TrustedProxies: []string{"127.0.0.1"},
			},
		}
		serverWithProxies := NewServer(cfgWithProxies, logger)
		serverWithProxies.InitializeRoutes()

		serverWithProxies.echo.GET("/ip", func(c echo.Context) error {
			// Check if IP extractor set the real IP
			if realIP := c.RealIP(); realIP != "" {
				return c.String(http.StatusOK, realIP)
			}
			return c.String(http.StatusNotFound, "no real_ip set")
		})

		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		// No X-Real-IP or X-Forwarded-For headers
		rec := httptest.NewRecorder()
		serverWithProxies.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		// Should fall back to RemoteAddr with port stripped
		if body != "127.0.0.1" {
			t.Errorf("Expected RealIP to fall back to '127.0.0.1' from RemoteAddr, got '%s'", body)
		}
	})

	t.Run("IP extractor strips port from RemoteAddr", func(t *testing.T) {
		logger := zap.NewNop()
		cfgWithProxies := &config.Config{
			Server: config.ServerConfig{
				Port:           8080,
				TrustedProxies: []string{"127.0.0.1"},
			},
		}
		serverWithProxies := NewServer(cfgWithProxies, logger)
		serverWithProxies.InitializeRoutes()

		serverWithProxies.echo.GET("/ip", func(c echo.Context) error {
			// Check if IP extractor set the real IP
			if realIP := c.RealIP(); realIP != "" {
				return c.String(http.StatusOK, realIP)
			}
			return c.String(http.StatusNotFound, "no real_ip set")
		})

		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "192.168.1.100:54321"
		// No X-Real-IP or X-Forwarded-For headers
		rec := httptest.NewRecorder()
		serverWithProxies.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		// Port should be stripped
		if body != "192.168.1.100" {
			t.Errorf("Expected RealIP to be '192.168.1.100' (port stripped), got '%s'", body)
		}
	})

	t.Run("IP extractor handles IPv6 addresses in RemoteAddr", func(t *testing.T) {
		logger := zap.NewNop()
		cfgWithProxies := &config.Config{
			Server: config.ServerConfig{
				Port:           8080,
				TrustedProxies: []string{"127.0.0.1"},
			},
		}
		serverWithProxies := NewServer(cfgWithProxies, logger)
		serverWithProxies.InitializeRoutes()

		serverWithProxies.echo.GET("/ip", func(c echo.Context) error {
			// Check if IP extractor set the real IP
			if realIP := c.RealIP(); realIP != "" {
				return c.String(http.StatusOK, realIP)
			}
			return c.String(http.StatusNotFound, "no real_ip set")
		})

		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "[2001:db8::1]:8080"
		// No X-Real-IP or X-Forwarded-For headers
		rec := httptest.NewRecorder()
		serverWithProxies.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		// IPv6 address with brackets and port should be handled correctly
		expected := "2001:db8::1"
		if body != expected {
			t.Errorf("Expected RealIP to be '%s', got '%s'", expected, body)
		}
	})

	t.Run("IP extractor handles IPv6 addresses in X-Real-IP", func(t *testing.T) {
		logger := zap.NewNop()
		cfgWithProxies := &config.Config{
			Server: config.ServerConfig{
				Port:           8080,
				TrustedProxies: []string{"127.0.0.1"},
			},
		}
		serverWithProxies := NewServer(cfgWithProxies, logger)
		serverWithProxies.InitializeRoutes()

		serverWithProxies.echo.GET("/ip", func(c echo.Context) error {
			// Check if IP extractor set the real IP
			if realIP := c.RealIP(); realIP != "" {
				return c.String(http.StatusOK, realIP)
			}
			return c.String(http.StatusNotFound, "no real_ip set")
		})

		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Real-IP", "2001:db8::1")
		rec := httptest.NewRecorder()
		serverWithProxies.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		expected := "2001:db8::1"
		if body != expected {
			t.Errorf("Expected RealIP to be '%s', got '%s'", expected, body)
		}
	})

	t.Run("IP extractor ignores invalid IP addresses", func(t *testing.T) {
		logger := zap.NewNop()
		cfgWithProxies := &config.Config{
			Server: config.ServerConfig{
				Port:           8080,
				TrustedProxies: []string{"127.0.0.1"},
			},
		}
		serverWithProxies := NewServer(cfgWithProxies, logger)
		serverWithProxies.InitializeRoutes()

		serverWithProxies.echo.GET("/ip", func(c echo.Context) error {
			// Check if IP extractor set the real IP
			if realIP := c.RealIP(); realIP != "" {
				return c.String(http.StatusOK, realIP)
			}
			return c.String(http.StatusNotFound, "no real_ip set")
		})

		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Real-IP", "invalid-ip-address")
		rec := httptest.NewRecorder()
		serverWithProxies.echo.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200 (should fall back to RemoteAddr), got %d", rec.Code)
		}
		body := rec.Body.String()
		if body != "127.0.0.1" {
			t.Errorf("Expected RealIP to fall back to '127.0.0.1' from RemoteAddr, got '%s'", body)
		}
	})
}

func TestServerStart(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           0,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)
	server.InitializeRoutes()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(":0")
	}()

	select {
	case err := <-errChan:
		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}
}

func TestServerShutdown(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           0,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)
	server.InitializeRoutes()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(":0")
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}

	select {
	case err := <-errChan:
		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Errorf("Start returned error after shutdown: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Server did not shut down within timeout")
	}
}
