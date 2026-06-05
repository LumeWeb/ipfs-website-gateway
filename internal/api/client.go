package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

type APIClient interface {
	GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
	Ping(ctx context.Context) (*ipfs.PingResponse, error)
}

type sdkClient struct {
	websites ipfs.WebsitesService
	ping     ipfs.PingService
}

func NewClient(baseURL, secret string, timeout time.Duration) (APIClient, error) {
	client, err := ipfs.NewClient(baseURL, "", ipfs.WithGatewaySecret(secret))
	if err != nil {
		return nil, fmt.Errorf("failed to create ipfs-sdk client: %w", err)
	}

	client.SetHTTPClient(&http.Client{
		Timeout: timeout,
	})

	return &sdkClient{
		websites: client.Websites(),
		ping:     client.Ping(),
	}, nil
}

func NewClientFromWebsitesService(websites ipfs.WebsitesService) APIClient {
	return &sdkClient{websites: websites}
}

func (c *sdkClient) GetWebsite(ctx context.Context, domain string) (_ *types.GatewayWebsiteResponse, err error) {
	ctx, span := otel.TraceMethod(ctx, "APIClient.GetWebsite",
		otel.WithAttributes(attribute.String("domain", domain)),
	)
	defer func() { otel.EndSpanWithErr(span, err) }()

	if domain == "" {
		return nil, fmt.Errorf("domain cannot be empty")
	}

	return c.websites.GetGatewayWebsite(ctx, domain)
}

func (c *sdkClient) Ping(ctx context.Context) (*ipfs.PingResponse, error) {
	if c.ping == nil {
		return nil, fmt.Errorf("ping service not configured")
	}
	return c.ping.Ping(ctx)
}
