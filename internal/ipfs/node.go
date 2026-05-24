package ipfs

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ipfs/boxo/bitswap"
	"github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/ipns"
	drclient "github.com/ipfs/boxo/routing/http/client"
	"github.com/ipfs/boxo/routing/http/contentrouter"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/routing"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubrouter "github.com/libp2p/go-libp2p-pubsub-router"
	madns "github.com/multiformats/go-multiaddr-dns"
	"github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

type Node struct {
	Host             host.Host
	BlockService     blockservice.BlockService
	Routing          routing.ValueStore
	Pubsub           *pubsub.PubSub
	pubsubValueStore *pubsubrouter.PubsubValueStore
	routingClient    *drclient.Client
	ctx              context.Context
	cancel           context.CancelFunc
	logger           *zap.Logger
}

func NewNode(ctx context.Context, seedPeer string, connectTimeout time.Duration, routingEndpoint string, bs blockstore.Blockstore, logger *zap.Logger, pubsubEnabled bool) (*Node, error) {
	if bs == nil {
		return nil, fmt.Errorf("blockstore cannot be nil")
	}

	nodeCtx, cancel := context.WithCancel(ctx)

	node := &Node{
		ctx:    nodeCtx,
		cancel: cancel,
		logger: logger,
	}

	host, err := node.initHost(nodeCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize libp2p host: %w", err)
	}

	node.Host = host

	// Create HTTP routing client for content routing and IPNS resolution
	var contentRouting routing.ContentRouting
	if routingEndpoint != "" {
		cli, err := drclient.New(
			routingEndpoint,
			drclient.WithProviderInfo(host.ID(), host.Addrs()),
		)
		if err != nil {
			logger.Warn("failed to create HTTP routing client, routing will be unavailable", zap.Error(err))
		} else {
			node.routingClient = cli
			contentRouting = contentrouter.NewContentRoutingClient(cli)
			logger.Info("HTTP routing client initialized", zap.String("endpoint", routingEndpoint))
		}
	}

	bsnetInstance := bsnet.NewFromIpfsHost(host)

	// Bitswap gets content routing (fixes nil bug!)
	bswapInstance := bitswap.New(nodeCtx, bsnetInstance, contentRouting, bs, bitswap.WithServerEnabled(false))

	node.BlockService = blockservice.New(bs, bswapInstance)

	if pubsubEnabled {
		gs, err := pubsub.NewGossipSub(nodeCtx, host,
			pubsub.WithMessageSigning(true),
			pubsub.WithStrictSignatureVerification(true),
		)
		if err != nil {
			logger.Warn("failed to initialize gossipsub, continuing without pubsub", zap.Error(err))
		} else {
			pvs, err := pubsubrouter.NewPubsubValueStore(nodeCtx, host, gs, ipns.Validator{KeyBook: host.Peerstore()})
			if err != nil {
				logger.Warn("failed to initialize pubsub value store, continuing without pubsub", zap.Error(err))
			} else {
				node.Pubsub = gs
				node.pubsubValueStore = pvs
				logger.Info("IPNS pubsub initialized")
			}
		}
	}

	// Set up routing: pubsub-first with HTTP routing fallback, or just HTTP routing
	if node.pubsubValueStore != nil && contentRouting != nil {
		node.Routing = newPubsubFirstRouting(node.pubsubValueStore, contentRouting.(routing.ValueStore))
	} else if contentRouting != nil {
		node.Routing = contentRouting.(routing.ValueStore)
	}

	// Connect to seed peer for Bitswap block fetching
	if seedPeer != "" {
		peerInfo, err := resolveSeedPeer(nodeCtx, seedPeer, connectTimeout)
		if err != nil {
			logger.Warn("failed to resolve seed peer", zap.String("peer", seedPeer), zap.Error(err))
		} else {
			if err := node.Host.Connect(nodeCtx, *peerInfo); err != nil {
				logger.Warn("failed to connect to seed peer", zap.String("peer", seedPeer), zap.Error(err))
			} else {
				logger.Info("connected to seed peer", zap.String("peer_id", peerInfo.ID.String()))
				// Persist the seed peer connection
				node.Host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, peerstore.PermanentAddrTTL)
			}
		}
	}

	logger.Info("IPFS node initialized",
		zap.String("peer_id", host.ID().String()),
		zap.Bool("pubsub", node.Pubsub != nil),
		zap.Bool("http_routing", contentRouting != nil),
	)

	return node, nil
}

func (n *Node) initHost(ctx context.Context) (host.Host, error) {
	h, err := libp2p.New(
		libp2p.UserAgent("ipfs-website-gateway/1.0.0"),
		libp2p.DisableRelay(),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	return h, nil
}

func resolveSeedPeer(ctx context.Context, seedPeer string, timeout time.Duration) (*peer.AddrInfo, error) {
	addr := seedPeer
	if !strings.HasPrefix(addr, "/") {
		addr = "/dnsaddr/" + addr
	}

	ma, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse seed peer address: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if madns.Matches(ma) {
		resolved, err := madns.Resolve(ctx, ma)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dnsaddr: %w", err)
		}
		infos, err := peer.AddrInfosFromP2pAddrs(resolved...)
		if err != nil || len(infos) == 0 {
			return nil, fmt.Errorf("no resolved addresses with peer ID found for %s", addr)
		}
		return &infos[0], nil
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return nil, fmt.Errorf("failed to extract peer info from address: %w", err)
	}

	return peerInfo, nil
}

func (n *Node) Close() error {
	n.logger.Info("shutting down IPFS node")

	var errs []error

	if n.cancel != nil {
		n.cancel()
	}

	if n.routingClient != nil {
		// The drclient.Client doesn't implement io.Closer directly,
		// but its internal http.Client can be closed via its transport.
		// For now, just nil the reference; the HTTP connections will be
		// cleaned up when the process exits.
		n.routingClient = nil
	}

	if n.Host != nil {
		if err := n.Host.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close host: %w", err))
		}
	}

	if n.BlockService != nil {
		if bsCloser, ok := n.BlockService.(io.Closer); ok {
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

func (n *Node) PeerID() peer.ID {
	return n.Host.ID()
}

func (n *Node) Addrs() []multiaddr.Multiaddr {
	return n.Host.Addrs()
}

func (n *Node) ConnectedPeers() []peer.ID {
	return n.Host.Network().Peers()
}

func CreateInMemoryBlockstore(ctx context.Context) (blockstore.Blockstore, error) {
	memDs := ds.NewMapDatastore()
	baseBs := blockstore.NewBlockstore(memDs)
	bs, err := blockstore.CachedBlockstore(ctx, baseBs, blockstore.DefaultCacheOpts())
	if err != nil {
		return nil, fmt.Errorf("failed to create cached blockstore: %w", err)
	}
	return bs, nil
}
