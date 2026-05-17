package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

// Mock DNS validator
type mockDNSValidator struct {
	validateFunc func(ctx context.Context, domain string) (string, error)
}

func (m *mockDNSValidator) ValidateDNSLink(ctx context.Context, domain string) (string, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, domain)
	}
	return "", errors.New("not implemented")
}

// Mock API client
type mockAPIClient struct {
	getWebsiteFunc func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
}

func (m *mockAPIClient) GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if m.getWebsiteFunc != nil {
		return m.getWebsiteFunc(ctx, domain)
	}
	return nil, errors.New("not implemented")
}

// Mock IPFS fetcher
type mockIPFSFetcher struct {
	fetchFunc func(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error)
}

func (m *mockIPFSFetcher) FetchUnixFile(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error) {
	if m.fetchFunc != nil {
		return m.fetchFunc(ctx, c, path)
	}
	return nil, "", errors.New("not implemented")
}

type mockReadSeekCloser struct {
	*strings.Reader
}

func newMockReadSeekCloser(content string) *mockReadSeekCloser {
	return &mockReadSeekCloser{
		Reader: strings.NewReader(content),
	}
}

func (m *mockReadSeekCloser) Close() error {
	return nil
}

func TestHandleGatewayRequest_ExtractsDomainFromHostHeader(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	validCID := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			if domain == "example.com" {
				return "/ipfs/" + validCID, nil
			}
			return "", errors.New("unexpected domain")
		},
	}
	mockAPI := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain:     domain,
				TargetType: "ipfs",
				TargetHash: validCID,
				Status:     types.StatusActive,
			}, nil
		},
	}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	mockFetcher := &mockIPFSFetcher{
		fetchFunc: func(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error) {
			return newMockReadSeekCloser("test content"), "index.html", nil
		},
	}

	server := &Server{
		logger:     logger,
		dns:        mockDNS,
		api:        mockAPI,
		statusCache: mockCache,
		fetcher:    mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body != "test content" {
		t.Errorf("Expected body 'test content', got '%s'", body)
	}

	// Verify cache was checked
	result := mockCache.Get("example.com")
	if !result.Hit {
		t.Error("Expected cache hit after request")
	}
}

func TestHandleGatewayRequest_Returns404WhenDNSLinkValidationFails(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "", errors.New("no DNSLink record found")
		},
	}
	mockAPI := &mockAPIClient{}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	mockFetcher := &mockIPFSFetcher{}

	server := &Server{
		logger:      logger,
		dns:         mockDNS,
		api:         mockAPI,
		statusCache: mockCache,
		fetcher:     mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
}

func TestHandleGatewayRequest_Returns404WhenAPINotFound(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	validCID := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "/ipfs/" + validCID, nil
		},
	}
	mockAPI := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return nil, errors.New("website not found: example.com")
		},
	}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	mockFetcher := &mockIPFSFetcher{}

	server := &Server{
		logger:      logger,
		dns:         mockDNS,
		api:         mockAPI,
		statusCache: mockCache,
		fetcher:     mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}

	// Verify negative cache was set
	result := mockCache.Get("example.com")
	if !result.Hit || result.Entry == nil {
		t.Error("Expected negative cache entry to be set")
	}
}

func TestHandleGatewayRequest_Returns410WhenWebsiteBroken(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	validCID := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "/ipfs/" + validCID, nil
		},
	}
	mockAPI := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return nil, errors.New("website is broken or gone: example.com")
		},
	}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	mockFetcher := &mockIPFSFetcher{}

	server := &Server{
		logger:      logger,
		dns:         mockDNS,
		api:         mockAPI,
		statusCache: mockCache,
		fetcher:     mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	if rec.Code != http.StatusGone {
		t.Errorf("Expected status 410, got %d", rec.Code)
	}
}

func TestHandleGatewayQuery_Returns502WhenIPFSFetchFails(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	validCID := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "/ipfs/" + validCID, nil
		},
	}
	mockAPI := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain:     domain,
				TargetType: "ipfs",
				TargetHash: validCID,
				Status:     types.StatusActive,
			}, nil
		},
	}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	mockFetcher := &mockIPFSFetcher{
		fetchFunc: func(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error) {
			return nil, "", errors.New("IPFS fetch failed")
		},
	}

	server := &Server{
		logger:      logger,
		dns:         mockDNS,
		api:         mockAPI,
		statusCache: mockCache,
		fetcher:     mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	if rec.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502, got %d", rec.Code)
	}
}

func TestHandleGatewayRequest_UsesCacheHit(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	validCID := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			// This should not be called on cache hit
			return "", errors.New("DNS should not be validated on cache hit")
		},
	}
	mockAPI := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			// This should not be called on cache hit
			return nil, errors.New("API should not be called on cache hit")
		},
	}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	
	// Pre-populate cache
	cachedResponse := &types.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: validCID,
		Status:     types.StatusActive,
	}
	mockCache.Set("example.com", cachedResponse)
	
	mockFetcher := &mockIPFSFetcher{
		fetchFunc: func(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error) {
			return newMockReadSeekCloser("cached content"), "index.html", nil
		},
	}

	server := &Server{
		logger:      logger,
		dns:         mockDNS,
		api:         mockAPI,
		statusCache: mockCache,
		fetcher:     mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body != "cached content" {
		t.Errorf("Expected body 'cached content', got '%s'", body)
	}
}

func TestHandleGatewayRequest_Returns500ForInternalError(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	validCID := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "/ipfs/" + validCID, nil
		},
	}
	mockAPI := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return nil, errors.New("unexpected internal error")
		},
	}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	mockFetcher := &mockIPFSFetcher{}

	server := &Server{
		logger:      logger,
		dns:         mockDNS,
		api:         mockAPI,
		statusCache: mockCache,
		fetcher:     mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}
}

func TestHandleGatewayRequest_SetsProperHeaders(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	validCID := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
	mockDNS := &mockDNSValidator{
		validateFunc: func(ctx context.Context, domain string) (string, error) {
			return "/ipfs/" + validCID, nil
		},
	}
	mockAPI := &mockAPIClient{
		getWebsiteFunc: func(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
			return &types.GatewayWebsiteResponse{
				Domain:     domain,
				TargetType: "ipfs",
				TargetHash: validCID,
				Status:     types.StatusActive,
			}, nil
		},
	}
	mockCache, _ := cache.NewStatusCache(100, time.Minute)
	content := "test content with proper headers"
	mockFetcher := &mockIPFSFetcher{
		fetchFunc: func(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error) {
			return newMockReadSeekCloser(content), "index.html", nil
		},
	}

	server := &Server{
		logger:      logger,
		dns:         mockDNS,
		api:         mockAPI,
		statusCache: mockCache,
		fetcher:     mockFetcher,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := server.HandleGatewayRequest(c)

	// Verify
	if err != nil {
		t.Fatalf("HandleGatewayRequest returned error: %v", err)
	}

	// Check Content-Type
	contentType := rec.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Expected Content-Type 'text/html; charset=utf-8', got '%s'", contentType)
	}

	// Check Content-Length
	contentLength := rec.Header().Get("Content-Length")
	expectedLength := strconv.Itoa(len(content))
	if contentLength != expectedLength {
		t.Errorf("Expected Content-Length '%s', got '%s'", expectedLength, contentLength)
	}

	// Check Cache-Control
	cacheControl := rec.Header().Get("Cache-Control")
	if cacheControl == "" {
		t.Error("Expected Cache-Control header to be set")
	}
}
