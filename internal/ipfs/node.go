package ipfs

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/boxo/bitswap"
	"github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/ipns"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	routinghelpers "github.com/libp2p/go-libp2p-routing-helpers"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubrouter "github.com/libp2p/go-libp2p-pubsub-router"
	madns "github.com/multiformats/go-multiaddr-dns"
	"github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

type Node struct {
	Host              host.Host
	BlockService      blockservice.BlockService
	Routing           routing.ValueStore
	Pubsub            *pubsub.PubSub
	routing           *routingProxy
	pubsubValueStore  *pubsubrouter.PubsubValueStore
	ctx               context.Context
	cancel            context.CancelFunc
	logger            *zap.Logger
	pubsubEnabled     bool

	seedPeerRetry struct {
		mu       sync.Mutex
		stopCh   chan struct{}
		running  bool
		peer     string
		timeout  time.Duration
	}
}

type routingProxy struct {
	mu      sync.RWMutex
	current routing.ValueStore
}

var _ routing.ValueStore = (*routingProxy)(nil)

func (p *routingProxy) PutValue(ctx context.Context, key string, value []byte, opts ...routing.Option) error {
	p.mu.RLock()
	vs := p.current
	p.mu.RUnlock()
	return vs.PutValue(ctx, key, value, opts...)
}

func (p *routingProxy) GetValue(ctx context.Context, key string, opts ...routing.Option) ([]byte, error) {
	p.mu.RLock()
	vs := p.current
	p.mu.RUnlock()
	return vs.GetValue(ctx, key, opts...)
}

func (p *routingProxy) SearchValue(ctx context.Context, key string, opts ...routing.Option) (<-chan []byte, error) {
	p.mu.RLock()
	vs := p.current
	p.mu.RUnlock()
	return vs.SearchValue(ctx, key, opts...)
}

func (p *routingProxy) swap(vs routing.ValueStore) {
	p.mu.Lock()
	p.current = vs
	p.mu.Unlock()
}

func (n *Node) swapRouting(dht routing.ValueStore) {
	var vs routing.ValueStore
	if n.pubsubValueStore != nil {
		vs = newPubsubFirstRouting(n.pubsubValueStore, dht)
	} else {
		vs = dht
	}
	n.routing.swap(vs)
}

func NewNode(ctx context.Context, seedPeer string, connectTimeout time.Duration, bs blockstore.Blockstore, logger *zap.Logger, pubsubEnabled bool) (*Node, error) {
	if bs == nil {
		return nil, fmt.Errorf("blockstore cannot be nil")
	}

	nodeCtx, cancel := context.WithCancel(ctx)

	node := &Node{
		ctx:           nodeCtx,
		cancel:        cancel,
		logger:        logger,
		routing:       &routingProxy{current: routinghelpers.Null{}},
		pubsubEnabled: pubsubEnabled,
	}
	node.Routing = node.routing

	host, err := node.initHost(nodeCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize libp2p host: %w", err)
	}

	node.Host = host

	bsnetInstance := bsnet.NewFromIpfsHost(host)

	bswapInstance := bitswap.New(nodeCtx, bsnetInstance, nil, bs, bitswap.WithServerEnabled(false))

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

	var connected bool
	if seedPeer != "" {
		peerInfo, err := resolveSeedPeer(nodeCtx, seedPeer, connectTimeout)
		if err != nil {
			logger.Warn("failed to resolve seed peer", zap.String("peer", seedPeer), zap.Error(err))
		} else {
			if err := node.Host.Connect(nodeCtx, *peerInfo); err != nil {
				logger.Warn("failed to connect to seed peer", zap.String("peer", seedPeer), zap.Error(err))
			} else {
				logger.Info("connected to seed peer", zap.String("peer_id", peerInfo.ID.String()))
				connected = true
			}
		}
	}

	if connected {
		spr, err := newSeedPeerRouting(nodeCtx, host)
		if err != nil {
			logger.Warn("failed to initialize seed peer routing", zap.Error(err))
		} else {
			if err := spr.Bootstrap(nodeCtx); err != nil {
				logger.Warn("failed to bootstrap DHT client", zap.Error(err))
			}
			node.swapRouting(spr)
		}
	}

	if seedPeer != "" && !connected {
		node.startSeedPeerRetry(seedPeer, connectTimeout)
	}

	logger.Info("IPFS node initialized",
		zap.String("peer_id", host.ID().String()),
		zap.Bool("pubsub", node.Pubsub != nil),
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

func (n *Node) startSeedPeerRetry(peer string, timeout time.Duration) {
	n.seedPeerRetry.mu.Lock()
	defer n.seedPeerRetry.mu.Unlock()

	if n.seedPeerRetry.running {
		return
	}

	n.seedPeerRetry.running = true
	n.seedPeerRetry.peer = peer
	n.seedPeerRetry.timeout = timeout
	n.seedPeerRetry.stopCh = make(chan struct{})

	go n.retrySeedPeerLoop()
}

func (n *Node) retrySeedPeerLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-n.seedPeerRetry.stopCh:
			return
		case <-ticker.C:
			peerInfo, err := resolveSeedPeer(n.ctx, n.seedPeerRetry.peer, n.seedPeerRetry.timeout)
			if err != nil {
				n.logger.Debug("seed peer retry: failed to resolve", zap.String("peer", n.seedPeerRetry.peer), zap.Error(err))
				continue
			}

			if err := n.Host.Connect(n.ctx, *peerInfo); err != nil {
				n.logger.Debug("seed peer retry: failed to connect", zap.String("peer", n.seedPeerRetry.peer), zap.Error(err))
				continue
			}

			n.logger.Info("seed peer retry: connected", zap.String("peer_id", peerInfo.ID.String()))

			spr, err := newSeedPeerRouting(n.ctx, n.Host)
			if err != nil {
				n.logger.Warn("seed peer retry: failed to initialize routing", zap.Error(err))
				continue
			}

			if err := spr.Bootstrap(n.ctx); err != nil {
				n.logger.Warn("seed peer retry: failed to bootstrap DHT", zap.Error(err))
			}

			n.seedPeerRetry.mu.Lock()
			n.swapRouting(spr)
			n.seedPeerRetry.running = false
			n.seedPeerRetry.mu.Unlock()

			return
		}
	}
}

func (n *Node) stopSeedPeerRetry() {
	n.seedPeerRetry.mu.Lock()
	defer n.seedPeerRetry.mu.Unlock()

	if n.seedPeerRetry.running && n.seedPeerRetry.stopCh != nil {
		close(n.seedPeerRetry.stopCh)
		n.seedPeerRetry.running = false
	}
}

func (n *Node) Close() error {
	n.logger.Info("shutting down IPFS node")

	n.stopSeedPeerRetry()

	var errs []error

	if n.cancel != nil {
		n.cancel()
	}

	n.routing.mu.RLock()
	routing := n.routing.current
	n.routing.mu.RUnlock()

	if routing != nil {
		if closer, ok := routing.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close routing: %w", err))
			}
		}
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
