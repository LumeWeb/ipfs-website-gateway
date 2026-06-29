package health

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
	ResolveIPNS(ctx context.Context, ipnsPath string) (cid.Cid, error)
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
	periodicOpts := []health.CheckerOption{
		health.WithTimeout(10 * time.Second),
	}

	// internal_api: periodic check
	periodicOpts = append(periodicOpts, health.WithPeriodicCheck(
		opts.Config.Interval, 0, health.Check{
			Name:    "internal_api",
			Timeout: 10 * time.Second,
			Check: timedCheck("internal_api", "", func(ctx context.Context) error {
				return checkAPIHealth(ctx, opts.PingSvc)
			}),
		},
	))

	// ipfs_peer: periodic check
	periodicOpts = append(periodicOpts, health.WithPeriodicCheck(
		opts.Config.Interval, 0, health.Check{
			Name:    "ipfs_peer",
			Timeout: 10 * time.Second,
			Check: timedCheck("ipfs_peer", "", func(ctx context.Context) error {
				return checkIPFSPeerHealth(ctx, opts.IPFSNode)
			}),
		},
	))

	// website_retrieval: periodic check, per-domain metrics
	if len(opts.Config.Websites) > 0 && opts.IPFSNode != nil && opts.DNSResolver != nil {
		periodicOpts = append(periodicOpts, health.WithPeriodicCheck(
			opts.Config.Interval, 0, health.Check{
				Name:    "website_retrieval",
				Timeout: opts.Config.Timeout,
				Check: timedCheck("website_retrieval", "", func(ctx context.Context) error {
					return checkWebsiteRetrieval(ctx, opts.DNSResolver, opts.IPFSNode, opts.Config.Websites, opts.Config.Timeout)
				}),
			},
		))
	}

	return health.NewChecker(periodicOpts...)
}

// timedCheck wraps a check function, records the duration, and updates
// Prometheus metrics. For the aggregate check status (domain="").
func timedCheck(checkName, domain string, fn func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		start := time.Now()
		err := fn(ctx)
		setCheckResult(checkName, domain, err == nil, time.Since(start).Seconds())
		return err
	}
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

func checkWebsiteRetrieval(ctx context.Context, resolver DNSLinkResolver, node IPFSNode, websites []string, timeout time.Duration) (err error) {
	ctx, span := otel.TraceMethod(ctx, "Health.checkWebsiteRetrieval")
	defer func() { otel.EndSpanWithErr(span, err) }()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)

	if len(websites) == 0 {
		return nil
	}

	// Give each website its own timeout derived from the parent, so one
	// slow domain cannot starve the others of time.
	perWebsite := timeout / time.Duration(len(websites))
	if perWebsite < 5*time.Second {
		perWebsite = 5 * time.Second
	}

	for _, domain := range websites {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			wctx, cancel := context.WithTimeout(ctx, perWebsite)
			defer cancel()
			start := time.Now()
			checkErr := checkSingleWebsite(wctx, resolver, node, d)
			setCheckResult("website_retrieval", d, checkErr == nil, time.Since(start).Seconds())
			if checkErr != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", d, checkErr))
				mu.Unlock()
			}
		}(domain)
	}
	wg.Wait()

	if len(failures) > 0 {
		return fmt.Errorf("website retrieval failed: %s", strings.Join(failures, "; "))
	}

	return nil
}

func checkSingleWebsite(ctx context.Context, resolver DNSLinkResolver, node IPFSNode, domain string) error {
	ipfsPath, err := resolver.ValidateDNSLink(ctx, domain)
	if err != nil {
		return fmt.Errorf("DNSLink resolution failed: %w", err)
	}

	rootCID, err := resolveRootCID(ctx, node, ipfsPath)
	if err != nil {
		return fmt.Errorf("failed to resolve root CID from %q: %w", ipfsPath, err)
	}

	blk, err := node.GetBlock(ctx, rootCID)
	if err != nil {
		return fmt.Errorf("block fetch failed for %s: %w", rootCID.String(), err)
	}
	if blk == nil {
		return fmt.Errorf("block fetch returned nil block for %s", rootCID.String())
	}

	return nil
}

// resolveRootCID extracts the root content CID from an IPFS path.
// For /ipfs/ paths the CID is parsed directly. For /ipns/ paths the
// IPNS record is resolved first via the node's routing layer to get
// the content CID.
func resolveRootCID(ctx context.Context, node IPFSNode, ipfsPath string) (cid.Cid, error) {
	parts := strings.SplitN(strings.TrimPrefix(ipfsPath, "/"), "/", 2)
	if len(parts) < 2 {
		return cid.Cid{}, fmt.Errorf("path too short")
	}

	switch parts[0] {
	case "ipfs":
		c, err := cid.Decode(parts[1])
		if err != nil {
			return cid.Cid{}, fmt.Errorf("invalid CID: %w", err)
		}
		return c, nil
	case "ipns":
		return node.ResolveIPNS(ctx, ipfsPath)
	default:
		return cid.Cid{}, fmt.Errorf("unsupported namespace %q: only /ipfs/ and /ipns/ paths are supported", parts[0])
	}
}
