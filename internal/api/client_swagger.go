package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	clientgen "go.lumeweb.com/ipfs-website-gateway/internal/client"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

// swaggerAPIClient wraps the generated swagger client to provide a clean abstraction.
type swaggerAPIClient struct {
	baseURL string
	secret  string
	client  *clientgen.ClientWithResponses
}

// NewSwaggerClient creates a new API client using the generated swagger client.
// The baseURL is the base URL of the internal API (e.g., "https://api.example.com").
// The secret is used for authentication via the X-Gateway-Secret header.
// The timeout controls how long HTTP requests can take before being canceled.
func NewSwaggerClient(baseURL, secret string, timeout time.Duration) APIClient {
	httpClient := &http.Client{Timeout: timeout}
	
	opts := []clientgen.ClientOption{
		clientgen.WithHTTPClient(httpClient),
	}
	
	swaggerClient, err := clientgen.NewClientWithResponses(baseURL, opts...)
	if err != nil {
		panic(fmt.Sprintf("failed to create swagger client: %v", err))
	}

	return &swaggerAPIClient{
		baseURL: baseURL,
		secret:  secret,
		client:  swaggerClient,
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
func (c *swaggerAPIClient) GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain cannot be empty")
	}

	resp, err := c.client.GetInternalWebsitesDomainWithResponse(ctx, domain,
		func(ctx context.Context, req *http.Request) error {
			req.Header.Set(gatewaySecretHeader, c.secret)
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("response body is empty")
		}
		return convertGatewayWebsiteResponse(resp.JSON200), nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("website not found: %s", domain)
	case http.StatusGone:
		return nil, fmt.Errorf("website is broken or gone: %s", domain)
	default:
		if resp.JSON401 != nil {
			return nil, fmt.Errorf("unauthorized: %s", resp.JSON401.Error)
		}
		if resp.JSON403 != nil {
			return nil, fmt.Errorf("forbidden: %s", resp.JSON403.Error)
		}
		if resp.JSON500 != nil {
			return nil, fmt.Errorf("server error: %s", resp.JSON500.Error)
		}
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}
}

// convertGatewayWebsiteResponse converts the generated client response to our internal type.
func convertGatewayWebsiteResponse(src *clientgen.GatewayWebsiteResponse) *types.GatewayWebsiteResponse {
	return &types.GatewayWebsiteResponse{
		Domain:     src.Domain,
		TargetType: src.TargetType,
		TargetHash: src.TargetHash,
		Status:     types.WebsiteStatus(src.Status),
	}
}
