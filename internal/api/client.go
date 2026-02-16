package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

const (
	// gatewaySecretHeader is the HTTP header used for API authentication.
	gatewaySecretHeader = "X-Gateway-Secret"

	// websitesEndpoint is the endpoint path for website configuration queries.
	websitesEndpoint = "/internal/websites/"
)

// APIClient defines the interface for API client implementations.
// This allows for different client implementations (direct HTTP, swagger-generated, etc.)
type APIClient interface {
	GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
}

// Client is an HTTP client for communicating with the internal API.
// It handles authentication via the X-Gateway-Secret header and provides
// methods to query website configuration.
type Client struct {
	baseURL string
	secret  string
	client  *http.Client
}

// NewClient creates a new API client with the specified configuration.
// The baseURL is the base URL of the internal API (e.g., "https://api.example.com").
// The secret is used for authentication via the X-Gateway-Secret header.
// The timeout controls how long HTTP requests can take before being canceled.
func NewClient(baseURL, secret string, timeout time.Duration) APIClient {
	return &Client{
		baseURL: baseURL,
		secret:  secret,
		client:  &http.Client{Timeout: timeout},
	}
}

// GetWebsite retrieves website configuration for the specified domain.
// It makes a GET request to {baseURL}/internal/websites/{domain} with
// the X-Gateway-Secret header for authentication.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - domain: The domain name to query (e.g., "example.com")
//
// Returns:
//   - *types.GatewayWebsiteResponse: The website configuration if found
//   - error: An error if the request fails, the website is not found (404),
//     the website is broken/gone (410), or another HTTP error occurs
func (c *Client) GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain cannot be empty")
	}

	url := c.baseURL + websitesEndpoint + domain

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(gatewaySecretHeader, c.secret)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var website types.GatewayWebsiteResponse
		if err := json.NewDecoder(resp.Body).Decode(&website); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		return &website, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("website not found: %s", domain)
	case http.StatusGone:
		return nil, fmt.Errorf("website is broken or gone: %s", domain)
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
