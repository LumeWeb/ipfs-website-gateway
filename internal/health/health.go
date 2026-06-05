package health

import (
	"context"
	"fmt"
	"time"

	"github.com/alexliesenfeld/health"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
)

type PingService interface {
	Ping(ctx context.Context) (*ipfs.PingResponse, error)
}

type IPFSNode interface {
	PeerID() peer.ID
	Addrs() []multiaddr.Multiaddr
	Close() error
	ConnectedPeers() []peer.ID
}

func NewChecker(pingSvc PingService, ipfsNode IPFSNode) health.Checker {
	return health.NewChecker(
		health.WithTimeout(10*time.Second),
		health.WithCheck(health.Check{
			Name: "internal_api",
			Check: func(ctx context.Context) error {
				return checkAPIHealth(ctx, pingSvc)
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

func checkAPIHealth(ctx context.Context, pingSvc PingService) (err error) {
	ctx, span := otel.TraceMethod(ctx, "Health.checkAPIHealth")
	defer func() { otel.EndSpanWithErr(span, err) }()

	_, err = pingSvc.Ping(ctx)
	if err != nil {
		return fmt.Errorf("internal API health check failed: %w", err)
	}
	return nil
}

func checkIPFSPeerHealth(ctx context.Context, node IPFSNode) (err error) {
	ctx, span := otel.TraceMethod(ctx, "Health.checkIPFSPeerHealth")
	defer func() { otel.EndSpanWithErr(span, err) }()

	if node == nil {
		return fmt.Errorf("IPFS node is not initialized")
	}

	addrs := node.Addrs()
	if len(addrs) == 0 {
		return fmt.Errorf("IPFS node is not listening on any addresses")
	}

	peers := node.ConnectedPeers()
	if len(peers) == 0 {
		return fmt.Errorf("IPFS node has no peer connections")
	}

	return nil
}
