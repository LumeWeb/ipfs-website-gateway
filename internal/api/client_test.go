package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

type mockWebsitesService struct {
	getGatewayWebsiteFunc func(ctx context.Context, domain string) (*ipfs.GatewayWebsiteResponse, error)
}

func (m *mockWebsitesService) List(ctx context.Context, opts ...ipfs.ListWebsitesOption) ([]ipfs.WebsiteItem, error) {
	return nil, nil
}

func (m *mockWebsitesService) Get(ctx context.Context, id string) (*ipfs.WebsiteResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) Create(ctx context.Context, domain string, targetHash string, targetType string) (*ipfs.WebsiteResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) Update(ctx context.Context, id string, domain string, targetHash string, targetType string) (*ipfs.WebsiteResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *mockWebsitesService) ValidateDNS(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) UpdateSSLStatusInternal(ctx context.Context, domain string, sslStatus ipfs.SSLStatusUpdateRequest) error {
	return nil
}

func (m *mockWebsitesService) GetGatewayWebsite(ctx context.Context, domain string) (*ipfs.GatewayWebsiteResponse, error) {
	if m.getGatewayWebsiteFunc != nil {
		return m.getGatewayWebsiteFunc(ctx, domain)
	}
	return nil, nil
}

func (m *mockWebsitesService) GetGatewayWebsiteStatus(ctx context.Context, domain string) (*ipfs.GatewayWebsiteStatusResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) WaitForSSLStatusReady(ctx context.Context, domain string, opts ...ipfs.PollOption) (string, error) {
	return "", nil
}

func (m *mockWebsitesService) WaitForWebsiteStatus(ctx context.Context, id string, expectedStatus string, opts ...ipfs.PollOption) error {
	return nil
}

func (m *mockWebsitesService) WaitForDNSValidation(ctx context.Context, id string, opts ...ipfs.PollOption) error {
	return nil
}

func (m *mockWebsitesService) GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
	return nil, nil
}

// The remaining methods satisfy the ipfs.WebsitesService interface added in
// ipfs-sdk v0.1.76 (domain binding). This mock is only used to test the
// gateway API client's GetWebsite path, so these return zero values.
func (m *mockWebsitesService) ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) UpdateDomain(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) UnbindDomain(ctx context.Context, websiteID string, domainID string) error {
	return nil
}

func (m *mockWebsitesService) VerifyDomain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) GetDomainDNSRequirements(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) RepublishDANE(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) ReconcileWebsiteChanges(ctx context.Context, after string) (*ipfs.WebsiteChangesResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) ConvertDomainToOnChain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) ListPlatformDomains(ctx context.Context) (*ipfs.PlatformDomainListResponse, error) {
	return nil, nil
}

func (m *mockWebsitesService) CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	return nil, nil
}

func TestSDKClient_GetWebsite_Success(t *testing.T) {
	mockSvc := &mockWebsitesService{
		getGatewayWebsiteFunc: func(ctx context.Context, domain string) (*ipfs.GatewayWebsiteResponse, error) {
			return &ipfs.GatewayWebsiteResponse{
				Domain:     "example.com",
				TargetType: "ipfs",
				TargetHash: "QmExample",
				Status:     "active",
			}, nil
		},
	}

	client := NewClientFromWebsitesService(mockSvc)

	resp, err := client.GetWebsite(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", resp.Domain)
	}
	if resp.TargetType != "ipfs" {
		t.Errorf("expected target type ipfs, got %s", resp.TargetType)
	}
	if resp.TargetHash != "QmExample" {
		t.Errorf("expected target hash QmExample, got %s", resp.TargetHash)
	}
	if resp.Status != types.StatusActive {
		t.Errorf("expected status active, got %s", resp.Status)
	}
}

func TestSDKClient_GetWebsite_Error(t *testing.T) {
	mockSvc := &mockWebsitesService{
		getGatewayWebsiteFunc: func(ctx context.Context, domain string) (*ipfs.GatewayWebsiteResponse, error) {
			return nil, errors.New("get gateway website failed with status 404")
		},
	}

	client := NewClientFromWebsitesService(mockSvc)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSDKClient_GetWebsite_EmptyDomain(t *testing.T) {
	mockSvc := &mockWebsitesService{}
	client := NewClientFromWebsitesService(mockSvc)

	_, err := client.GetWebsite(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty domain, got nil")
	}
	if err.Error() != "domain cannot be empty" {
		t.Errorf("expected 'domain cannot be empty', got '%s'", err.Error())
	}
}

func TestSDKClient_TypePassthrough(t *testing.T) {
	mockSvc := &mockWebsitesService{
		getGatewayWebsiteFunc: func(ctx context.Context, domain string) (*ipfs.GatewayWebsiteResponse, error) {
			return &ipfs.GatewayWebsiteResponse{
				Domain:     "test.com",
				TargetType: "ipns",
				TargetHash: "k51qzi5uqu5djuc7yel4lzixq3e6ifsm0n1v0lrug8g9o18n4r0v2bgfjlkekm",
				Status:     "broken",
			}, nil
		},
	}

	client := NewClientFromWebsitesService(mockSvc)

	resp, err := client.GetWebsite(context.Background(), "test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != types.StatusBroken {
		t.Errorf("expected status broken, got %s", resp.Status)
	}
	if resp.TargetType != "ipns" {
		t.Errorf("expected target type ipns, got %s", resp.TargetType)
	}
}

// TestNewClient_PingUsesFreshConnections guards the health-check hardening:
// the ping path must not reuse stale keep-alive connections (a pooled TLS
// connection to the edge can silently die and hang /healthz until its
// timeout). Each Ping must establish a fresh connection, matching a direct
// curl to /internal/ping.
//
// We count TCP connections accepted by the test server rather than handler
// invocations: each Ping always sends its own HTTP request regardless of
// connection reuse, so handler counts would still read 3 even if keep-alive
// connections were fully reused. One Accept per established TCP connection.
func TestNewClient_PingUsesFreshConnections(t *testing.T) {
	var accepts atomic.Int64

	// connCountingListener wraps the httptest listener so we see every
	// accepted TCP connection (one Accept per established connection).
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	l := &connCountingListener{Listener: inner, accepts: &accepts}
	defer l.Close()

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/ping" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	client, err := NewClient(ts.URL, "test-secret", 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	checks := 3
	first := accepts.Load()
	for i := 0; i < checks; i++ {
		if _, err := client.Ping(context.Background()); err != nil {
			t.Fatalf("Ping() attempt %d error = %v", i+1, err)
		}
	}

	got := accepts.Load() - first
	if got != int64(checks) {
		t.Errorf("expected %d freshly accepted TCP connections (one per ping), got %d (connections are being reused)", checks, got)
	}
}

// connCountingListener wraps a net.Listener and increments a counter for
// every accepted connection, so tests can distinguish connection reuse from
// request counting.
type connCountingListener struct {
	net.Listener
	accepts *atomic.Int64
}

func (l *connCountingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.accepts.Add(1)
	}
	return conn, err
}
