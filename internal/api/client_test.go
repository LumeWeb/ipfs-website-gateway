package api

import (
	"context"
	"errors"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

type mockWebsitesService struct {
	getGatewayWebsiteFunc func(ctx context.Context, domain string) (*ipfs.GatewayWebsiteResponse, error)
}

func (m *mockWebsitesService) List(ctx context.Context) ([]ipfs.WebsiteItem, error) {
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
