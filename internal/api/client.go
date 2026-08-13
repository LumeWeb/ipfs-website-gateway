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

const (
	// idleConnTimeout mirrors the ipfs-sdk default. It bounds how long an idle
	// pooled connection is kept alive so stale keep-alive connections to the
	// portal edge (which Caddy on-demand TLS can silently close) are reaped and
	// re-dialed instead of being reused and hanging the request.
	idleConnTimeout = 90 * time.Second
)

// hardenedTransport returns a clone of http.DefaultTransport with a finite idle
// connection lifetime and a bounded idle pool. Unlike http.DefaultTransport
// (whose idle connections linger forever), this reaps connections that go stale
// after server restarts or edge idle timeouts, preventing pooled-connection
// hangs in the shared client. Standard behavior (proxy from environment,
// dial/TLS timeouts, HTTP/2) is preserved.
func hardenedTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.IdleConnTimeout = idleConnTimeout
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	return t
}

func NewClient(baseURL, secret string, timeout time.Duration) (APIClient, error) {
	client, err := ipfs.NewClient(baseURL, "", ipfs.WithGatewaySecret(secret))
	if err != nil {
		return nil, fmt.Errorf("failed to create ipfs-sdk client: %w", err)
	}

	// Apply the configured timeout while preserving a hardened transport. A
	// bare &http.Client{Timeout: t} would drop the SDK's hardened default and
	// fall back to http.DefaultTransport's unbounded idle pool, which is what
	// allowed stale keep-alive connections to hang GetWebsite requests in
	// production.
	client.SetHTTPClient(&http.Client{
		Timeout:   timeout,
		Transport: hardenedTransport(),
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
	pt := http.DefaultTransport.(*http.Transport).Clone()
	pt.DisableKeepAlives = true
	pingClient.SetHTTPClient(&http.Client{
		Timeout:   timeout,
		Transport: pt,
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
