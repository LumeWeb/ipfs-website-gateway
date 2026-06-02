package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"
	"go.lumeweb.com/ipfs-website-gateway/internal/config"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

func TestAllowedHandler_ValidatesDomain(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	tests := []struct {
		name           string
		domain         string
		expectedStatus int
	}{
		{
			name:           "valid domain example.com",
			domain:         "example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid domain with subdomain",
			domain:         "www.example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid domain with multiple subdomains",
			domain:         "api.v1.example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid domain with hyphen",
			domain:         "my-domain.example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid domain with numbers",
			domain:         "domain123.example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "empty domain returns 400",
			domain:         "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain starting with hyphen returns 400",
			domain:         "-invalid.example.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain ending with hyphen returns 400",
			domain:         "invalid-.example.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with spaces returns 400",
			domain:         "invalid domain.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with invalid characters returns 400",
			domain:         "invalid_domain.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with at symbol returns 400",
			domain:         "user@domain.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with slash returns 400",
			domain:         "domain.com/path",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with colon returns 400",
			domain:         "domain.com:8080",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with consecutive dots returns 400",
			domain:         "domain..example.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain starting with dot returns 400",
			domain:         ".domain.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain ending with dot returns 400",
			domain:         "domain.com.",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with only dots returns 400",
			domain:         "...",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "domain with uppercase letters should be valid (case insensitive)",
			domain:         "Example.COM",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "single label domain",
			domain:         "localhost",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "domain with underscore (invalid)",
			domain:         "test_domain.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "IPv4 address returns 400",
			domain:         "172.18.0.2",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "IPv6 address returns 400",
			domain:         "::1",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "IPv6 full address returns 400",
			domain:         "2001:db8::1",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape(tt.domain), nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Execute
			err := server.allowedHandler(c)

			// Verify
			if err != nil {
				t.Fatalf("allowedHandler returned error: %v", err)
			}

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestAllowedHandler_LogsRequest(t *testing.T) {
	// Create a logger that captures debug logs
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	t.Run("logs domain and client IP", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Execute
		err := server.allowedHandler(c)

		// Verify
		if err != nil {
			t.Fatalf("allowedHandler returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

func TestAllowedHandler_UsesRequestContext(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	t.Run("handler uses request context", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Verify that request context is available
		if c.Request().Context() == nil {
			t.Fatal("Request context should not be nil")
		}

		// Execute
		err := server.allowedHandler(c)

		// Verify
		if err != nil {
			t.Fatalf("allowedHandler returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

func TestAllowedHandler_ReturnsOKForValidDomain(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// The handler should return 200 OK for valid domains
	// (Future tasks will add actual domain validation logic)
}

func TestAllowedHandler_HandlesXRealIPHeader(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{"127.0.0.1"},
		},
	}

	server := NewServer(cfg, logger)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Real-IP", "10.0.0.1")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Verify that RealIP is extracted correctly
	realIP := c.RealIP()
	if realIP != "10.0.0.1" {
		t.Errorf("Expected RealIP to be '10.0.0.1', got '%s'", realIP)
	}
}

func TestAllowedHandler_MissingDomainParameter(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing domain parameter, got %d", rec.Code)
	}
}

func TestAllowedHandler_DomainTooLong(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	// Create a domain that exceeds 253 characters (RFC 1035 limit)
	longDomain := ""
	for i := 0; i < 260; i++ {
		longDomain += "a"
	}
	longDomain += ".com"

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+longDomain, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for domain exceeding 253 characters, got %d", rec.Code)
	}
}

func TestAllowedHandler_DomainLabelTooLong(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	// Create a domain with a label exceeding 63 characters (RFC 1035 limit)
	longLabel := ""
	for i := 0; i < 70; i++ {
		longLabel += "a"
	}
	domain := longLabel + ".com"

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+domain, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for domain label exceeding 63 characters, got %d", rec.Code)
	}
}

// MockDNSValidator is a mock implementation of DNSValidator for testing.
type MockDNSValidator struct {
	validateFunc func(ctx context.Context, domain string) (string, error)
}

func (m *MockDNSValidator) ValidateDNSLink(ctx context.Context, domain string) (string, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, domain)
	}
	return "", nil
}

func TestAllowedHandler_DNSLinkValidation_Success(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	// Set up mock DNS validator that returns a valid IPFS path
	mockDNS := &MockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "/ipfs/QmSomeCID", nil
		},
	}
	server.SetDNSValidator(mockDNS)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for valid DNSLink, got %d", rec.Code)
	}
}

func TestAllowedHandler_DNSLinkValidation_Returns403OnError(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	// Set up mock DNS validator that returns an error
	mockDNS := &MockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "", fmt.Errorf("DNS query failed")
		},
	}
	server.SetDNSValidator(mockDNS)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for DNSLink validation error, got %d", rec.Code)
	}
}

func TestAllowedHandler_DNSLinkValidation_Returns400ForInvalidPath(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	// Set up mock DNS validator that returns an empty path (no error)
	mockDNS := &MockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "", nil
		},
	}
	server.SetDNSValidator(mockDNS)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid DNSLink path, got %d", rec.Code)
	}
}

func TestAllowedHandler_DNSLinkValidation_UsesRequestContext(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	// Set up mock DNS validator that checks context is passed
	contextPassed := false
	mockDNS := &MockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			contextPassed = (ctx != nil)
			return "/ipfs/QmSomeCID", nil
		},
	}
	server.SetDNSValidator(mockDNS)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if !contextPassed {
		t.Error("Expected context to be passed to DNS validator")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestAllowedHandler_APIStatusFailure(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	tests := []struct {
		name           string
		domain         string
		apiError       error
		expectedStatus int
	}{
		{
			name:           "website not found",
			domain:         "notfound.example.com",
			apiError:       fmt.Errorf("website not found: notfound.example.com"),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "website is broken or gone",
			domain:         "broken.example.com",
			apiError:       fmt.Errorf("website is broken or gone: broken.example.com"),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "generic API error",
			domain:         "error.example.com",
			apiError:       fmt.Errorf("internal server error"),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "network timeout error",
			domain:         "timeout.example.com",
			apiError:       fmt.Errorf("context deadline exceeded"),
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(cfg, logger)

			// Set up mock DNS validator that returns a valid IPFS path
			mockDNS := &MockDNSValidator{
				validateFunc: func(ctx context.Context, domain string) (string, error) {
					return "/ipfs/QmSomeCID", nil
				},
			}
			server.SetDNSValidator(mockDNS)

			// Set up mock API client that returns error
			mockAPI := &MockAPIClient{
				getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
					return nil, tt.apiError
				},
			}
			server.SetAPIClient(mockAPI)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape(tt.domain), nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Execute
			err := server.allowedHandler(c)

			// Verify
			if err != nil {
				t.Fatalf("allowedHandler returned error: %v", err)
			}

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d for %s, got %d", tt.expectedStatus, tt.name, rec.Code)
			}
		})
	}
}

func TestAllowedHandler_InactiveWebsite(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	tests := []struct {
		name           string
		domain         string
		websiteStatus  string
		expectedStatus int
	}{
		{
			name:           "website with broken status",
			domain:         "broken.example.com",
			websiteStatus:  types.StatusBroken,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "website with pending validation status",
			domain:         "pending.example.com",
			websiteStatus:  types.StatusPendingValidation,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(cfg, logger)

			// Set up mock DNS validator that returns a valid IPFS path
			mockDNS := &MockDNSValidator{
				validateFunc: func(ctx context.Context, domain string) (string, error) {
					return "/ipfs/QmSomeCID", nil
				},
			}
			server.SetDNSValidator(mockDNS)

			// Set up mock API client that returns inactive website
			mockAPI := &MockAPIClient{
				getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
					return &types.GatewayWebsiteResponse{Status: tt.websiteStatus}, nil
				},
			}
			server.SetAPIClient(mockAPI)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape(tt.domain), nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Execute
			err := server.allowedHandler(c)

			// Verify
			if err != nil {
				t.Fatalf("allowedHandler returned error: %v", err)
			}

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d for %s, got %d", tt.expectedStatus, tt.name, rec.Code)
			}
		})
	}
}

// MockAPIClient is a mock implementation of APIClient for testing.
type MockAPIClient struct {
	getWebsiteFunc func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
}

func (m *MockAPIClient) GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if m.getWebsiteFunc != nil {
		return m.getWebsiteFunc(ctx, domain)
	}
	return nil, nil
}

func TestAllowedHandler_Success(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	tests := []struct {
		name   string
		domain string
	}{
		{
			name:   "valid domain example.com",
			domain: "example.com",
		},
		{
			name:   "valid domain with subdomain",
			domain: "www.example.com",
		},
		{
			name:   "valid domain with multiple subdomains",
			domain: "api.v1.example.com",
		},
		{
			name:   "valid domain with hyphen",
			domain: "my-domain.example.com",
		},
		{
			name:   "valid domain with numbers",
			domain: "domain123.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(cfg, logger)

			// Set up mock DNS validator that returns a valid IPFS path
			mockDNS := &MockDNSValidator{
				validateFunc: func(ctx context.Context, domain string) (string, error) {
					return "/ipfs/QmSomeCID", nil
				},
			}
			server.SetDNSValidator(mockDNS)

			// Set up mock API client that returns active website
			mockAPI := &MockAPIClient{
				getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
					return &types.GatewayWebsiteResponse{Status: types.StatusActive}, nil
				},
			}
			server.SetAPIClient(mockAPI)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape(tt.domain), nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Execute
			err := server.allowedHandler(c)

			// Verify
			if err != nil {
				t.Fatalf("allowedHandler returned error: %v", err)
			}

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status 200 for valid domain, got %d", rec.Code)
			}
		})
	}
}

func TestAllowedHandler_DNSLinkFail_APIActiveFallback(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	mockDNS := &MockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "", fmt.Errorf("DNS query failed: SERVFAIL")
		},
	}
	server.SetDNSValidator(mockDNS)

	mockAPI := &MockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain:     "example.com",
				Status:     types.StatusActive,
				TargetType: "ipns",
				TargetHash: "12D3KooWTest",
			}, nil
		},
	}
	server.SetAPIClient(mockAPI)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := server.allowedHandler(c)

	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 (portal fallback), got %d", rec.Code)
	}
}

func TestAllowedHandler_DNSLinkFail_APINotActive_Rejects(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	mockDNS := &MockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "", fmt.Errorf("DNS query failed: SERVFAIL")
		},
	}
	server.SetDNSValidator(mockDNS)

	mockAPI := &MockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain: "example.com",
				Status: types.StatusBroken,
			}, nil
		},
	}
	server.SetAPIClient(mockAPI)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := server.allowedHandler(c)

	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 (broken site), got %d", rec.Code)
	}
}

func TestAllowedHandler_DNSLinkValidation_LogsSuccess(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			TrustedProxies: []string{},
		},
	}

	server := NewServer(cfg, logger)

	// Set up mock DNS validator
	mockDNS := &MockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "/ipfs/QmSomeCID", nil
		},
	}
	server.SetDNSValidator(mockDNS)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/allowed?domain="+url.QueryEscape("example.com"), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute - this should log successful DNSLink validation at debug level
	err := server.allowedHandler(c)

	// Verify
	if err != nil {
		t.Fatalf("allowedHandler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
