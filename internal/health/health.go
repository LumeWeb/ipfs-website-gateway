package health

import (
	"context"
	"fmt"
	"time"

	"github.com/alexliesenfeld/health"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

// APIClient defines the interface for querying the internal API.
type APIClient interface {
	GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
}

// IPFSNode defines the interface for IPFS node operations needed for health checks.
type IPFSNode interface {
	PeerID() peer.ID
	Addrs() []multiaddr.Multiaddr
	Close() error
	ConnectedPeers() []peer.ID
}

// NewChecker creates a new health checker with checks for internal API and IPFS peer connectivity.
// The checker monitors the health of critical system components and returns aggregated status.
func NewChecker(apiClient APIClient, ipfsNode IPFSNode) health.Checker {
	return health.NewChecker(
		health.WithTimeout(10*time.Second),
		health.WithCheck(health.Check{
			Name: "internal_api",
			Check: func(ctx context.Context) error {
				return checkAPIHealth(ctx, apiClient)
			},
		}),
		health.WithCheck(health.Check{
			Name: "ipfs_peer",
			Check: func(ctx context.Context) error {
				return checkIPFSPeerHealth(ctx, ipfsNode)
			},
		}),
	)
}

// checkAPIHealth verifies that the internal API is reachable and responding.
// It attempts to query a known endpoint (using a test domain) to verify connectivity.
func checkAPIHealth(ctx context.Context, client APIClient) error {
	// Use a minimal request to check API connectivity
	// We query a health check domain that should exist in the API
	// The actual response content doesn't matter, just that we get a response
	_, err := client.GetWebsite(ctx, "health-check.example.com")
	
	// If we get a 404, the API is up but the domain doesn't exist - that's OK for health check
	// We only fail on connection errors or other unexpected errors
	if err != nil {
		errStr := err.Error()
		// Consider 404 as healthy (API is responding)
		if errStr == "website not found: health-check.example.com" {
			return nil
		}
		// Consider 410 as healthy (API is responding)
		if errStr == "website is broken or gone: health-check.example.com" {
			return nil
		}
		// Any other error indicates a problem
		return fmt.Errorf("internal API health check failed: %w", err)
	}
	
	return nil
}

// checkIPFSPeerHealth verifies that the IPFS node has at least one peer connection.
// It checks if the node has connected peers in its peer store.
func checkIPFSPeerHealth(ctx context.Context, node IPFSNode) error {
	if node == nil {
		return fmt.Errorf("IPFS node is not initialized")
	}

	// Check if the node has any addresses (indicates it's listening)
	addrs := node.Addrs()
	if len(addrs) == 0 {
		return fmt.Errorf("IPFS node is not listening on any addresses")
	}

	// Check for active peer connections
	peers := node.ConnectedPeers()
	if len(peers) == 0 {
		return fmt.Errorf("IPFS node has no peer connections")
	}

	return nil
}
