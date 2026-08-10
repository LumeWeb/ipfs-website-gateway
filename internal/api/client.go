package api

import (
	"context"
	"fmt"
	"net/http"
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
	client, err := ipfs.NewClient(baseURL, "", ipfs.WithGatewaySecret(secret))
	if err != nil {
		return nil, fmt.Errorf("failed to create ipfs-sdk client: %w", err)
	}

	client.SetHTTPClient(&http.Client{
		Timeout: timeout,
	})

	// Dedicated health-check client. The shared client pools keep-alive
	// connections, which can silently go stale on the TLS edge (Caddy on-demand
	// certs / idle timeouts) and hang the health check until its timeout. The
	// gateway's own /healthz must not be held hostage by a dead pooled
	// connection, so the ping path opens a fresh connection per check, matching
	// a direct curl to /internal/ping. Clone the default transport so standard
	// behavior (proxy from environment, dial/TLS timeouts, HTTP/2) is preserved.
	pingClient, err := ipfs.NewClient(baseURL, "", ipfs.WithGatewaySecret(secret))
	if err != nil {
		return nil, fmt.Errorf("failed to create ipfs-sdk ping client: %w", err)
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	pingClient.SetHTTPClient(&http.Client{
		Timeout:   timeout,
		Transport: t,
	})

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
