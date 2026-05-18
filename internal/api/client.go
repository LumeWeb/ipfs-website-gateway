package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

type APIClient interface {
	GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
}

type sdkClient struct {
	websites ipfs.WebsitesService
}

func NewClient(baseURL, secret string, timeout time.Duration) (APIClient, error) {
	client, err := ipfs.NewClient(baseURL, "", ipfs.WithGatewaySecret(secret))
	if err != nil {
		return nil, fmt.Errorf("failed to create ipfs-sdk client: %w", err)
	}

	client.SetHTTPClient(&http.Client{
		Timeout: timeout,
	})

	return &sdkClient{websites: client.Websites()}, nil
}

func NewClientFromWebsitesService(websites ipfs.WebsitesService) APIClient {
	return &sdkClient{websites: websites}
}

func (c *sdkClient) GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain cannot be empty")
	}

	return c.websites.GetGatewayWebsite(ctx, domain)
}
