package api

import (
	"context"
	"fmt"
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.opentelemetry.io/otel/attribute"
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
	client, err := ipfs.NewClient(baseURL, "", ipfs.WithGatewaySecret(secret), ipfs.WithTimeout(timeout))
	if err != nil {
		return nil, fmt.Errorf("failed to create ipfs-sdk client: %w", err)
	}

	// Dedicated health-check client. The shared client pools keep-alive
	// connections, which can silently go stale on the TLS edge (Caddy on-demand
	// certs / idle timeouts) and hang the health check until its timeout. The
	// gateway's own /healthz must not be held hostage by a dead pooled
	// connection, so the ping path opens a fresh connection per check, matching
	// a direct curl to /internal/ping. WithKeepAlive(false) gives a fresh
	// connection per request while preserving the SDK's hardened transport,
	// so no hand-constructed transport / SetHTTPClient is needed.
	pingClient, err := ipfs.NewClient(baseURL, "",
		ipfs.WithGatewaySecret(secret),
		ipfs.WithTimeout(timeout),
		ipfs.WithKeepAlive(false),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ipfs-sdk ping client: %w", err)
	}

	return &sdkClient{
		websites: client.Websites(),
		ping:     pingClient.Ping(),
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
