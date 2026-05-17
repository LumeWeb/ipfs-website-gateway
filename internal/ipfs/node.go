package ipfs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipfs/boxo/bitswap"
	"github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	madns "github.com/multiformats/go-multiaddr-dns"
	"github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

// Node wraps a minimal Boxo IPFS node with the components needed for content fetching.
// It provides access to libp2p host, blockstore, and blockservice for IPFS operations.
//
// This is a minimal implementation designed for content fetching and caching,
// not a full-featured IPFS daemon. It uses Boxo components to create a lightweight
// node that can connect to the IPFS network and retrieve content.
type Node struct {
	// Host is the libp2p host for peer-to-peer networking
	Host host.Host

	// Blockservice handles block retrieval via Bitswap
	Blockservice blockservice.BlockService

	// ctx is the context for the node's lifetime
	ctx context.Context

	// cancel is used to shutdown the node gracefully
	cancel context.CancelFunc

	// logger is used for logging node operations
	logger *zap.Logger

	// repoPath is the filesystem path for IPFS data storage
	repoPath string
}

// NewNode creates a minimal Boxo IPFS node with the specified configuration.
//
// The seedPeer parameter specifies a peer address to connect to for initial
// bootstrap (e.g., "ipfs.pinner.xyz"). If empty, the node will start without
// bootstrap connections and rely on other discovery methods.
//
// The repoPath parameter specifies the directory path for storing IPFS data
// including the blockstore and configuration. The directory will be created
// if it does not exist.
//
// Returns an error if repoPath is empty, the directory cannot be created,
// or any component initialization fails.
func NewNode(ctx context.Context, seedPeer string, repoPath string, logger *zap.Logger) (*Node, error) {
	if repoPath == "" {
		return nil, fmt.Errorf("repo path cannot be empty")
	}

	// Create repo directory if it doesn't exist
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Create a child context with cancellation for node lifecycle
	nodeCtx, cancel := context.WithCancel(ctx)

	node := &Node{
		ctx:      nodeCtx,
		cancel:   cancel,
		logger:   logger,
		repoPath: repoPath,
	}

	// Initialize datastore (blockstore backend)
	ds, err := node.initDatastore(repoPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize datastore: %w", err)
	}

	// Initialize blockstore
	bs, err := node.initBlockstore(ds)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize blockstore: %w", err)
	}

	// Initialize libp2p host
	host, err := node.initHost(nodeCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize libp2p host: %w", err)
	}

	node.Host = host

	// Create bitswap network from host
	bsnet := bsnet.NewFromIpfsHost(host)

	// Initialize bitswap for content exchange
	// For minimal node without DHT, providerFinder is nil
	bswapInstance := bitswap.New(nodeCtx, bsnet, nil, bs, bitswap.WithServerEnabled(false))

	// Initialize blockservice
	node.Blockservice = blockservice.New(bs, bswapInstance)

	// Connect to seed peer if specified
	if seedPeer != "" {
		if err := node.connectToSeedPeer(nodeCtx, seedPeer); err != nil {
			logger.Warn("failed to connect to seed peer", zap.String("peer", seedPeer), zap.Error(err))
			// Don't fail initialization if seed peer connection fails
		}
	}

	logger.Info("IPFS node initialized",
		zap.String("peer_id", host.ID().String()),
		zap.String("repo_path", repoPath),
	)

	return node, nil
}

// initDatastore creates and returns a datastore for IPFS data storage.
// It uses a simple in-memory datastore for minimal setup.
// TODO: Integrate with persistent blockstore from cache package in Phase 7.
func (n *Node) initDatastore(repoPath string) (ds.Batching, error) {
	// Use a simple map-based datastore for now
	// This will be replaced with the persistent blockstore in Phase 7
	return ds.NewMapDatastore(), nil
}

// initBlockstore creates and returns a blockstore backed by the datastore.
// TODO: Integrate with persistent blockstore from cache package in Phase 7.
func (n *Node) initBlockstore(ds ds.Batching) (blockstore.Blockstore, error) {
	// Create a simple blockstore wrapper
	// This will be replaced with the persistent blockstore in Phase 7
	return blockstore.NewBlockstore(ds), nil
}

// initHost creates and returns a libp2p host for peer-to-peer networking.
// It uses a minimal configuration suitable for content fetching.
func (n *Node) initHost(ctx context.Context) (host.Host, error) {
	// Create libp2p host with minimal options
	// Use random ports and default transports
	h, err := libp2p.New(
		libp2p.UserAgent("ipfs-website-gateway/0.1.0"),
		libp2p.DisableRelay(),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	return h, nil
}

// connectToSeedPeer attempts to connect to a specified seed peer address.
// The seedPeer parameter can be a full multiaddr with peer ID (e.g.,
// "/dns/ipfs.pinner.xyz/tcp/443/wss/p2p/Qm...") or a plain DNS name (e.g.,
// "ipfs.pinner.xyz") which will be resolved via dnsaddr.
func (n *Node) connectToSeedPeer(ctx context.Context, seedPeer string) error {
	addr := seedPeer
	if !strings.HasPrefix(addr, "/") {
		addr = "/dnsaddr/" + addr
	}

	ma, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return fmt.Errorf("failed to parse seed peer address: %w", err)
	}

	var peerInfo *peer.AddrInfo

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if madns.Matches(ma) {
		resolved, err := madns.Resolve(ctx, ma)
		if err != nil {
			return fmt.Errorf("failed to resolve dnsaddr: %w", err)
		}
		// AddrInfosFromP2pAddrs is all-or-nothing: returns (nil, ErrInvalidAddr)
		// if any addr lacks /p2p/, or (ais, nil) with all grouped by peer ID.
		infos, err := peer.AddrInfosFromP2pAddrs(resolved...)
		if err != nil || len(infos) == 0 {
			return fmt.Errorf("no resolved addresses with peer ID found for %s", addr)
		}
		peerInfo = &infos[0]
	} else {
		peerInfo, err = peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			return fmt.Errorf("failed to extract peer info from address: %w", err)
		}
	}

	if err := n.Host.Connect(ctx, *peerInfo); err != nil {
		return fmt.Errorf("failed to connect to peer %s: %w", peerInfo.ID, err)
	}

	n.logger.Info("connected to seed peer",
		zap.String("peer_id", peerInfo.ID.String()),
		zap.String("address", ma.String()),
	)

	return nil
}

// Close gracefully shuts down the IPFS node and releases all resources.
// It closes the libp2p host, blockservice, and datastore.
// After calling Close, the Node should not be used.
func (n *Node) Close() error {
	n.logger.Info("shutting down IPFS node")

	var errs []error

	// Cancel the node context to stop all operations
	if n.cancel != nil {
		n.cancel()
	}

	// Close libp2p host
	if n.Host != nil {
		if err := n.Host.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close host: %w", err))
		}
	}

	// Close blockservice
	if n.Blockservice != nil {
		if bsCloser, ok := n.Blockservice.(interface{ Close() error }); ok {
			if err := bsCloser.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close blockservice: %w", err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}

	n.logger.Info("IPFS node shutdown complete")
	return nil
}

// PeerID returns the libp2p peer ID of this node.
func (n *Node) PeerID() peer.ID {
	return n.Host.ID()
}

// Addrs returns the list of multiaddresses that this node is listening on.
func (n *Node) Addrs() []multiaddr.Multiaddr {
	return n.Host.Addrs()
}

// ConnectedPeers returns the list of peer IDs that this node is currently connected to.
func (n *Node) ConnectedPeers() []peer.ID {
	return n.Host.Network().Peers()
}
