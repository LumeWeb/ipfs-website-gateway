package ipfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"crypto/rand"

	"github.com/avast/retry-go/v5"
	"github.com/decred/go-bip39"
	"github.com/ipfs/boxo/bitswap"
	"github.com/ipfs/boxo/bitswap/client"
	"github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/ipns"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	drclient "github.com/ipfs/boxo/routing/http/client"
	"github.com/ipfs/boxo/routing/http/contentrouter"
	blocks "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubrouter "github.com/libp2p/go-libp2p-pubsub-router"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
	"go.opentelemetry.io/otel/attribute"
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

	// Seed peer reconnection worker
	seedPeerMu             sync.Mutex
	seedPeerAddr           string
	seedPeerCancel         context.CancelFunc
	seedPeerActive         bool
	seedPeerConnectTimeout time.Duration
	seedPeerWg             sync.WaitGroup
	seedPeerWorkerID       [8]byte
	seedPeerID             peer.ID
	seedPeerConnected      atomic.Bool
}

func NewNode(ctx context.Context, seedPeer string, connectTimeout time.Duration, routingEndpoint string, bs blockstore.Blockstore, logger *zap.Logger, pubsubEnabled bool, seed string) (*Node, error) {
	if bs == nil {
		return nil, fmt.Errorf("blockstore cannot be nil")
	}

	if !bip39.IsMnemonicValid(seed) {
		return nil, fmt.Errorf("invalid BIP-39 mnemonic")
	}

	nodeCtx, cancel := context.WithCancel(ctx)

	node := &Node{
		ctx:    nodeCtx,
		cancel: cancel,
		logger: logger.Named("ipfs"),
	}

	host, err := node.initHost(seed)
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
			node.logger.Warn("HTTP routing client creation failed, routing unavailable",
				zap.String("endpoint", routingEndpoint),
				zap.Error(err),
			)
		} else {
			node.routingClient = cli
			contentRouting = contentrouter.NewContentRoutingClient(cli)
			node.logger.Info("HTTP routing client initialized",
				zap.String("endpoint", routingEndpoint),
				zap.String("peer_id", host.ID().String()),
			)
		}
	} else {
		node.logger.Warn("no routing endpoint configured, IPNS resolution will be unavailable")
	}

	bsnetInstance := bsnet.NewFromIpfsHost(host)

	bswapTracer := newDebugTracer(node.logger)
	var bswapOpts []bitswap.Option
	bswapOpts = append(bswapOpts,
		bitswap.WithServerEnabled(false),
		bitswap.WithTracer(bswapTracer),
	)
	if node.logger.Core().Enabled(zap.DebugLevel) {
		bswapOpts = append(bswapOpts, bitswap.WithClientOption(client.WithTraceBlock(true)))
	}

	bswapInstance := bitswap.New(nodeCtx, bsnetInstance, contentRouting, bs, bswapOpts...)

	innerBS := blockservice.New(bs, bswapInstance)
	if node.logger.Core().Enabled(zap.DebugLevel) {
		node.BlockService = newLoggingBlockService(innerBS, node.logger)
	} else {
		node.BlockService = innerBS
	}

	if pubsubEnabled {
		gs, err := pubsub.NewGossipSub(nodeCtx, host,
			pubsub.WithMessageSigning(true),
			pubsub.WithStrictSignatureVerification(true),
		)
		if err != nil {
			node.logger.Warn("gossipsub initialization failed, continuing without pubsub", zap.Error(err))
		} else {
			pvs, err := pubsubrouter.NewPubsubValueStore(nodeCtx, host, gs, ipns.Validator{KeyBook: host.Peerstore()})
			if err != nil {
				node.logger.Warn("pubsub value store initialization failed, continuing without pubsub", zap.Error(err))
			} else {
				node.Pubsub = gs
				node.pubsubValueStore = pvs
				node.logger.Info("pubsub value store initialized")
			}
		}
	}

	// Set up routing: pubsub-first with HTTP routing fallback, or just HTTP routing
	if node.pubsubValueStore != nil && contentRouting != nil {
		node.Routing = newPubsubFirstRouting(node.pubsubValueStore, contentRouting.(routing.ValueStore))
	} else if contentRouting != nil {
		node.Routing = contentRouting.(routing.ValueStore)
	}

	node.logger.Info("routing configured",
		zap.String("type", fmt.Sprintf("%T", node.Routing)),
		zap.Bool("http_routing", contentRouting != nil),
		zap.Bool("pubsub", node.pubsubValueStore != nil),
	)

	// Connect to seed peer for Bitswap block fetching
	if seedPeer != "" {
		node.seedPeerAddr = seedPeer
		node.seedPeerConnectTimeout = connectTimeout
		node.ConnectSeedPeer()
	}

	node.logger.Info("node initialized",
		zap.String("peer_id", host.ID().String()),
		zap.Int("addrs", len(host.Addrs())),
		zap.Bool("pubsub", node.Pubsub != nil),
		zap.Bool("http_routing", contentRouting != nil),
		zap.String("routing_type", fmt.Sprintf("%T", node.Routing)),
	)

	return node, nil
}

func (n *Node) initHost(seed string) (host.Host, error) {
	derivedSeed := bip39.NewSeed(seed, "")
	privKey, _, err := crypto.GenerateEd25519Key(bytes.NewReader(derivedSeed))
	if err != nil {
		return nil, fmt.Errorf("failed to derive identity from seed: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.UserAgent("ipfs-website-gateway/1.0.0"),
		libp2p.DisableRelay(),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
		libp2p.PrometheusRegisterer(prometheus.WrapRegistererWithPrefix("libp2p_", metrics.Registry())),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	return h, nil
}

func resolveSeedPeer(ctx context.Context, seedPeer string, timeout time.Duration) (_ *peer.AddrInfo, err error) {
	ctx, span := otel.TraceMethod(ctx, "Node.resolveSeedPeer",
		otel.WithAttributes(attribute.String("seed_peer", seedPeer)),
	)
	defer func() { otel.EndSpanWithErr(span, err) }()

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
		resolved, err := resolveDNSRecursive(ctx, ma)
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

func resolveDNSRecursive(ctx context.Context, ma multiaddr.Multiaddr) ([]multiaddr.Multiaddr, error) {
	resolved, err := madns.Resolve(ctx, ma)
	if err != nil {
		return nil, err
	}

	var (
		result  []multiaddr.Multiaddr
		lastErr error
	)
	for _, addr := range resolved {
		if madns.Matches(addr) {
			recursed, err := resolveDNSRecursive(ctx, addr)
			if err != nil {
				lastErr = err
				continue
			}
			result = append(result, recursed...)
		} else {
			result = append(result, addr)
		}
	}

	if len(result) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return result, nil
}

const (
	seedPeerInitialBackoff = 1 * time.Second
	seedPeerMaxBackoff     = 60 * time.Second
	seedPeerReconnectDelay = 5 * time.Second
)

// seedPeerWorker runs a persistent loop that:
//  1. Resolves and connects to the seed peer (with retries + backoff)
//  2. Registers a networkNotify to detect disconnects
//  3. On disconnect, re-enters the connect loop
//
// The worker exits only when ctx is cancelled.
func (n *Node) seedPeerWorker(ctx context.Context, workerID [8]byte) {
	ctx, span := otel.TraceMethod(ctx, "Node.seedPeerWorker",
		otel.WithAttributes(attribute.String("seed_peer", n.seedPeerAddr)),
	)
	defer span.End()

	defer func() {
		n.seedPeerMu.Lock()
		if n.seedPeerWorkerID == workerID {
			n.seedPeerActive = false
			n.seedPeerCancel = nil
		}
		n.seedPeerMu.Unlock()
		n.seedPeerWg.Done()
	}()

	disconnected := make(chan struct{}, 1)

	for {
		if ctx.Err() != nil {
			return
		}

		// Phase 1: connect with retries + backoff
		peerInfo, err := n.connectSeedPeerWithRetry(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			n.logger.Error("seed peer worker exhausted retries, will retry from scratch", zap.Error(err))
			n.seedPeerConnected.Store(false)
			seedPeerConnected.Set(0)
			select {
			case <-ctx.Done():
				return
			case <-time.After(seedPeerMaxBackoff):
				continue
			}
		}

		n.seedPeerMu.Lock()
		n.seedPeerID = peerInfo.ID
		n.seedPeerMu.Unlock()

		// Phase 2: monitor for disconnect
		var notifyOnce sync.Once
		notify := &network.NotifyBundle{
			DisconnectedF: func(_ network.Network, conn network.Conn) {
				if conn.RemotePeer() == peerInfo.ID {
					notifyOnce.Do(func() {
						select {
						case disconnected <- struct{}{}:
						default:
						}
					})
				}
			},
		}
		n.Host.Network().Notify(notify)
		n.seedPeerConnected.Store(true)
		seedPeerConnected.Set(1)

		// Check if already disconnected (race between connect and notify)
		if !n.isConnectedTo(peerInfo.ID) {
			notifyOnce.Do(func() {
				select {
				case disconnected <- struct{}{}:
				default:
				}
			})
		}

		n.logger.Info("seed peer connected, monitoring for disconnects",
			zap.String("peer_id", peerInfo.ID.String()))

		// Phase 3: wait for disconnect or shutdown
		select {
		case <-ctx.Done():
			n.Host.Network().StopNotify(notify)
			n.seedPeerConnected.Store(false)
			seedPeerConnected.Set(0)
			return
		case <-disconnected:
			n.Host.Network().StopNotify(notify)
			n.seedPeerConnected.Store(false)
			seedPeerConnected.Set(0)
			seedPeerDisconnects.Inc()
			n.logger.Warn("seed peer disconnected, will reconnect",
				zap.String("peer_id", peerInfo.ID.String()))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(seedPeerReconnectDelay):
		}
	}
}

// connectSeedPeerWithRetry resolves and connects to the seed peer, retrying
// with exponential backoff. It also populates the peerstore and protects the
// connection BEFORE attempting to connect, so libp2p has the addresses
// available for reconnection.
func (n *Node) connectSeedPeerWithRetry(ctx context.Context) (*peer.AddrInfo, error) {
	var lastPeerInfo *peer.AddrInfo

	err := retry.New(
		retry.Context(ctx),
		retry.Attempts(0),
		retry.Delay(seedPeerInitialBackoff),
		retry.MaxDelay(seedPeerMaxBackoff),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(attempt uint, err error) {
			seedPeerConnectErrors.Inc()
			n.logger.Warn("seed peer connect failed, retrying",
				zap.String("peer", n.seedPeerAddr),
				zap.Uint("attempt", attempt),
				zap.Error(err),
			)
		}),
	).Do(func() error {
		seedPeerConnectAttempts.Inc()

		peerInfo, err := resolveSeedPeer(ctx, n.seedPeerAddr, n.seedPeerConnectTimeout)
		if err != nil {
			return fmt.Errorf("resolve: %w", err)
		}

		// Populate peerstore and protect BEFORE connecting, so libp2p
		// has the addresses available for its own reconnection logic.
		n.Host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, peerstore.PermanentAddrTTL)
		n.Host.ConnManager().Protect(peerInfo.ID, "seed-peer")
		n.Host.ConnManager().TagPeer(peerInfo.ID, "seed-peer", 100)

		if err := n.Host.Connect(ctx, *peerInfo); err != nil {
			return fmt.Errorf("connect: %w", err)
		}

		lastPeerInfo = peerInfo
		return nil
	})

	if err != nil {
		return nil, err
	}
	return lastPeerInfo, nil
}

// isConnectedTo returns true if the host has an active connection to the
// given peer.
func (n *Node) isConnectedTo(p peer.ID) bool {
	for _, conn := range n.Host.Network().ConnsToPeer(p) {
		if !conn.IsClosed() {
			return true
		}
	}
	return false
}

// SeedPeerConnected reports whether the seed peer is currently connected.
func (n *Node) SeedPeerConnected() bool {
	return n.seedPeerConnected.Load()
}

func (n *Node) ConnectSeedPeer() {
	_, span := otel.TraceMethod(n.ctx, "Node.ConnectSeedPeer",
		otel.WithAttributes(attribute.String("seed_peer", n.seedPeerAddr)),
	)
	defer span.End()

	n.seedPeerMu.Lock()
	defer n.seedPeerMu.Unlock()

	if n.seedPeerAddr == "" || n.seedPeerActive {
		return
	}

	ctx, cancel := context.WithCancel(n.ctx)
	n.seedPeerCancel = cancel
	n.seedPeerActive = true
	var workerID [8]byte
	if _, err := rand.Read(workerID[:]); err != nil {
		n.logger.Error("failed to generate worker ID", zap.Error(err))
		n.seedPeerActive = false
		n.seedPeerCancel = nil
		cancel()
		return
	}
	n.seedPeerWorkerID = workerID

	n.seedPeerWg.Add(1)
	go n.seedPeerWorker(ctx, n.seedPeerWorkerID)
}

func (n *Node) DisconnectSeedPeer() {
	n.seedPeerMu.Lock()
	cancel := n.seedPeerCancel
	n.seedPeerCancel = nil
	n.seedPeerActive = false
	n.seedPeerMu.Unlock()

	if cancel != nil {
		cancel()
	}
	n.seedPeerWg.Wait()
}

func (n *Node) Close() error {
	n.logger.Info("shutting down IPFS node")

	var errs []error

	n.DisconnectSeedPeer()

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

func (n *Node) GetBlock(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	return n.BlockService.GetBlock(ctx, c)
}

// ResolveIPNS resolves an /ipns/{name} path to the root content CID using
// the node's routing layer (HTTP routing + pubsub). This is the same
// resolution path the gateway uses for production IPNS requests.
func (n *Node) ResolveIPNS(ctx context.Context, ipnsPath string) (cid.Cid, error) {
	if n.Routing == nil {
		return cid.Cid{}, fmt.Errorf("routing unavailable: no routing endpoint configured")
	}

	p, err := path.NewPath(ipnsPath)
	if err != nil {
		return cid.Cid{}, fmt.Errorf("invalid IPNS path %q: %w", ipnsPath, err)
	}

	resolver := namesys.NewIPNSResolver(n.Routing)
	result, err := resolver.Resolve(ctx, p)
	if err != nil {
		return cid.Cid{}, fmt.Errorf("IPNS resolution failed for %q: %w", ipnsPath, err)
	}

	c, err := cid.Decode(strings.TrimPrefix(result.Path.String(), "/ipfs/"))
	if err != nil {
		return cid.Cid{}, fmt.Errorf("resolved path %q does not contain a valid CID: %w", result.Path.String(), err)
	}

	return c, nil
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
