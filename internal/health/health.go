package health

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexliesenfeld/health"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	ipfs "go.lumeweb.com/ipfs-sdk"
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
	SeedPeerConnected() bool
	GetBlock(ctx context.Context, c cid.Cid) (blocks.Block, error)
}

// DNSLinkResolver resolves a domain to its IPFS path via DNSLink.
type DNSLinkResolver interface {
	ValidateDNSLink(ctx context.Context, domain string) (string, error)
}

type HealthCheckConfig struct {
	Websites []string
	Interval time.Duration
	Timeout  time.Duration
}

type CheckerOptions struct {
	PingSvc     PingService
	IPFSNode    IPFSNode
	DNSResolver DNSLinkResolver
	Config      HealthCheckConfig
}

func NewChecker(opts CheckerOptions) health.Checker {
	checks := []health.CheckerOption{
		health.WithTimeout(10 * time.Second),
		health.WithCheck(health.Check{
			Name: "internal_api",
			Check: func(ctx context.Context) error {
				return checkAPIHealth(ctx, opts.PingSvc)
			},
		}),
		health.WithCheck(health.Check{
			Name: "ipfs_peer",
			Check: func(ctx context.Context) error {
				return checkIPFSPeerHealth(ctx, opts.IPFSNode)
			},
		}),
	}

	// Website retrieval check: for each configured website, resolves DNSLink
	// to get the root CID, then fetches that block via GetBlock. Tests DNS
	// resolution, DNSLink parsing, and bitswap retrieval (gateway -> seed
	// peer -> Sia) in one check.
	if len(opts.Config.Websites) > 0 && opts.IPFSNode != nil && opts.DNSResolver != nil {
		checks = append(checks, health.WithCheck(health.Check{
			Name:    "website_retrieval",
			Timeout: opts.Config.Timeout,
			Check: func(ctx context.Context) error {
				return checkWebsiteRetrieval(ctx, opts.DNSResolver, opts.IPFSNode, opts.Config.Websites)
			},
		}))
	}

	return health.NewChecker(checks...)
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

	if !node.SeedPeerConnected() {
		return fmt.Errorf("seed peer is not connected")
	}

	return nil
}

func checkWebsiteRetrieval(ctx context.Context, resolver DNSLinkResolver, node IPFSNode, websites []string) (err error) {
	ctx, span := otel.TraceMethod(ctx, "Health.checkWebsiteRetrieval")
	defer func() { otel.EndSpanWithErr(span, err) }()

	var failures []string
	for _, domain := range websites {
		if err := checkSingleWebsite(ctx, resolver, node, domain); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", domain, err))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("website retrieval failed: %s", strings.Join(failures, "; "))
	}

	return nil
}

func checkSingleWebsite(ctx context.Context, resolver DNSLinkResolver, node IPFSNode, domain string) error {
	// Step 1: Resolve DNSLink to get the IPFS path (e.g. /ipfs/Qm...).
	ipfsPath, err := resolver.ValidateDNSLink(ctx, domain)
	if err != nil {
		return fmt.Errorf("DNSLink resolution failed: %w", err)
	}

	// Step 2: Extract the root CID from the path.
	rootCID, err := extractCIDFromPath(ipfsPath)
	if err != nil {
		return fmt.Errorf("invalid DNSLink path %q: %w", ipfsPath, err)
	}

	// Step 3: Fetch the root block via bitswap. This exercises the full
	// gateway -> seed peer (portal) -> Sia retrieval path.
	blk, err := node.GetBlock(ctx, rootCID)
	if err != nil {
		return fmt.Errorf("block fetch failed for %s: %w", rootCID.String(), err)
	}
	if blk == nil {
		return fmt.Errorf("block fetch returned nil block for %s", rootCID.String())
	}

	return nil
}

// extractCIDFromPath parses an IPFS path like "/ipfs/Qm..." and returns the
// root CID. Returns an error if the path is malformed or the CID is invalid.
func extractCIDFromPath(ipfsPath string) (cid.Cid, error) {
	// Strip the /ipfs/ or /ipns/ prefix.
	parts := strings.SplitN(strings.TrimPrefix(ipfsPath, "/"), "/", 2)
	if len(parts) < 2 {
		return cid.Cid{}, fmt.Errorf("path too short")
	}

	c, err := cid.Decode(parts[1])
	if err != nil {
		return cid.Cid{}, fmt.Errorf("invalid CID: %w", err)
	}

	return c, nil
}
