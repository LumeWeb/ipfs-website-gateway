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
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	madns "github.com/multiformats/go-multiaddr-dns"
	"github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

type Node struct {
	Host         host.Host
	BlockService blockservice.BlockService
	Routing      routing.ValueStore
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *zap.Logger
}

func NewNode(ctx context.Context, seedPeer string, connectTimeout time.Duration, bs blockstore.Blockstore, logger *zap.Logger) (*Node, error) {
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

	bsnetInstance := bsnet.NewFromIpfsHost(host)

	bswapInstance := bitswap.New(nodeCtx, bsnetInstance, nil, bs, bitswap.WithServerEnabled(false))

	node.BlockService = blockservice.New(bs, bswapInstance)

	var seedID peer.ID
	if seedPeer != "" {
		peerInfo, err := resolveSeedPeer(nodeCtx, seedPeer, connectTimeout)
		if err != nil {
			logger.Warn("failed to resolve seed peer", zap.String("peer", seedPeer), zap.Error(err))
		} else {
			seedID = peerInfo.ID
			if err := node.Host.Connect(nodeCtx, *peerInfo); err != nil {
				logger.Warn("failed to connect to seed peer", zap.String("peer", seedPeer), zap.Error(err))
			} else {
				logger.Info("connected to seed peer", zap.String("peer_id", peerInfo.ID.String()))
			}
		}
	}

	if len(seedID) > 0 {
		spr, err := newSeedPeerRouting(nodeCtx, host, seedID)
		if err != nil {
			logger.Warn("failed to initialize seed peer routing", zap.Error(err))
		} else {
			node.Routing = spr
		}
	}

	logger.Info("IPFS node initialized",
		zap.String("peer_id", host.ID().String()),
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
