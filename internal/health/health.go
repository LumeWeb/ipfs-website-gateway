package health

import (
	"context"
	"fmt"
	"strings"
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
	_, err := client.GetWebsite(ctx, "health-check.example.com")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "gone") || strings.Contains(errStr, "status 404") || strings.Contains(errStr, "status 410") {
			return nil
		}
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
