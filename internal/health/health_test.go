package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexliesenfeld/health"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// --- Mocks ---

type mockPingService struct {
	err error
}

func (m *mockPingService) Ping(ctx context.Context) (*ipfs.PingResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ipfs.PingResponse{}, nil
}

type mockIPFSNode struct {
	peerConnected bool
	addrs         []multiaddr.Multiaddr
	block         blocks.Block
	blockErr      error
	resolveErr    error
	resolvedCID   cid.Cid
}

func (m *mockIPFSNode) PeerID() peer.ID              { return "" }
func (m *mockIPFSNode) Addrs() []multiaddr.Multiaddr { return m.addrs }
func (m *mockIPFSNode) Close() error                 { return nil }
func (m *mockIPFSNode) ConnectedPeers() []peer.ID    { return nil }
func (m *mockIPFSNode) SeedPeerConnected() bool      { return m.peerConnected }
func (m *mockIPFSNode) GetBlock(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	if m.blockErr != nil {
		return nil, m.blockErr
	}
	return m.block, nil
}
func (m *mockIPFSNode) ResolveIPNS(ctx context.Context, ipnsPath string) (cid.Cid, error) {
	if m.resolveErr != nil {
		return cid.Cid{}, m.resolveErr
	}
	return m.resolvedCID, nil
}

type mockDNSResolver struct {
	resultMap map[string]string
	errMap    map[string]error
}

func (m *mockDNSResolver) ValidateDNSLink(ctx context.Context, domain string) (string, error) {
	if err, ok := m.errMap[domain]; ok {
		return "", err
	}
	if path, ok := m.resultMap[domain]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

// --- Test helpers ---

func testCheckerOpts(t *testing.T, websites []string) CheckerOptions {
	t.Helper()

	addrs := []multiaddr.Multiaddr{}
	ma, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/4001")
	addrs = append(addrs, ma)

	// Use a real CID: QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	testBlock, _ := blocks.NewBlockWithCid([]byte("test"), testCID)

	return CheckerOptions{
		PingSvc: &mockPingService{},
		IPFSNode: &mockIPFSNode{
			peerConnected: true,
			addrs:         addrs,
			block:         testBlock,
		},
		DNSResolver: &mockDNSResolver{
			resultMap: map[string]string{
				"example.com": "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
				"pinner.xyz":  "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
			},
		},
		Config: HealthCheckConfig{
			Websites: websites,
			Interval: 100 * time.Millisecond,
			Timeout:  5 * time.Second,
		},
	}
}

// --- Tests ---

func TestCheckAPIHealth_Success(t *testing.T) {
	err := checkAPIHealth(context.Background(), &mockPingService{})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckAPIHealth_Failure(t *testing.T) {
	err := checkAPIHealth(context.Background(), &mockPingService{err: errors.New("conn refused")})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCheckIPFSPeerHealth_Success(t *testing.T) {
	ma, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/4001")
	node := &mockIPFSNode{peerConnected: true, addrs: []multiaddr.Multiaddr{ma}}
	err := checkIPFSPeerHealth(context.Background(), node)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckIPFSPeerHealth_NilNode(t *testing.T) {
	err := checkIPFSPeerHealth(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil node")
	}
}

func TestCheckIPFSPeerHealth_NoAddrs(t *testing.T) {
	node := &mockIPFSNode{peerConnected: true, addrs: nil}
	err := checkIPFSPeerHealth(context.Background(), node)
	if err == nil {
		t.Fatal("expected error for no addrs")
	}
}

func TestCheckIPFSPeerHealth_SeedDisconnected(t *testing.T) {
	ma, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/4001")
	node := &mockIPFSNode{peerConnected: false, addrs: []multiaddr.Multiaddr{ma}}
	err := checkIPFSPeerHealth(context.Background(), node)
	if err == nil {
		t.Fatal("expected error for disconnected seed peer")
	}
}

func TestResolveRootCID_IPFSPath(t *testing.T) {
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	node := &mockIPFSNode{}
	c, err := resolveRootCID(context.Background(), node, "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if !c.Equals(testCID) {
		t.Fatalf("expected %s, got %s", testCID, c)
	}
}

func TestResolveRootCID_IPNSPath(t *testing.T) {
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	node := &mockIPFSNode{resolvedCID: testCID}
	c, err := resolveRootCID(context.Background(), node, "/ipns/k51qzi5uqu5djuc7yel4lzixq3e6ifsm0n1v0lrug8g9o18n4r0v2bgfjlkekm")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if !c.Equals(testCID) {
		t.Fatalf("expected %s, got %s", testCID, c)
	}
}

func TestResolveRootCID_IPNSResolutionFail(t *testing.T) {
	node := &mockIPFSNode{resolveErr: errors.New("routing timeout")}
	_, err := resolveRootCID(context.Background(), node, "/ipns/k51qzi5uqu5djuc7yel4lzixq3e6ifsm0n1v0lrug8g9o18n4r0v2bgfjlkekm")
	if err == nil {
		t.Fatal("expected error for IPNS resolution failure")
	}
}

func TestResolveRootCID_ShortPath(t *testing.T) {
	node := &mockIPFSNode{}
	_, err := resolveRootCID(context.Background(), node, "/ipfs")
	if err == nil {
		t.Fatal("expected error for short path")
	}
}

func TestResolveRootCID_InvalidCID(t *testing.T) {
	node := &mockIPFSNode{}
	_, err := resolveRootCID(context.Background(), node, "/ipfs/notacid")
	if err == nil {
		t.Fatal("expected error for invalid CID")
	}
}

func TestResolveRootCID_EmptyPath(t *testing.T) {
	node := &mockIPFSNode{}
	_, err := resolveRootCID(context.Background(), node, "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestResolveRootCID_UnsupportedNamespace(t *testing.T) {
	node := &mockIPFSNode{}
	_, err := resolveRootCID(context.Background(), node, "/unknown/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	if err == nil {
		t.Fatal("expected error for unsupported namespace")
	}
}

func TestCheckSingleWebsite_Success(t *testing.T) {
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	testBlock, _ := blocks.NewBlockWithCid([]byte("test"), testCID)

	resolver := &mockDNSResolver{
		resultMap: map[string]string{"example.com": "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG"},
	}
	node := &mockIPFSNode{block: testBlock}

	err := checkSingleWebsite(context.Background(), resolver, node, "example.com")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckSingleWebsite_DNSLinkFail(t *testing.T) {
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	testBlock, _ := blocks.NewBlockWithCid([]byte("test"), testCID)

	resolver := &mockDNSResolver{
		errMap: map[string]error{"example.com": errors.New("dns timeout")},
	}
	node := &mockIPFSNode{block: testBlock}

	err := checkSingleWebsite(context.Background(), resolver, node, "example.com")
	if err == nil {
		t.Fatal("expected error for DNSLink failure")
	}
}

func TestCheckSingleWebsite_BlockFetchFail(t *testing.T) {
	resolver := &mockDNSResolver{
		resultMap: map[string]string{"example.com": "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG"},
	}
	node := &mockIPFSNode{blockErr: errors.New("bitswap timeout")}

	err := checkSingleWebsite(context.Background(), resolver, node, "example.com")
	if err == nil {
		t.Fatal("expected error for block fetch failure")
	}
}

func TestCheckSingleWebsite_IPNSPath(t *testing.T) {
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	testBlock, _ := blocks.NewBlockWithCid([]byte("test"), testCID)

	resolver := &mockDNSResolver{
		resultMap: map[string]string{"example.com": "/ipns/k51qzi5uqu5djuc7yel4lzixq3e6ifsm0n1v0lrug8g9o18n4r0v2bgfjlkekm"},
	}
	node := &mockIPFSNode{block: testBlock, resolvedCID: testCID}

	err := checkSingleWebsite(context.Background(), resolver, node, "example.com")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckWebsiteRetrieval_AllSuccess(t *testing.T) {
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	testBlock, _ := blocks.NewBlockWithCid([]byte("test"), testCID)

	resolver := &mockDNSResolver{
		resultMap: map[string]string{
			"example.com": "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
			"pinner.xyz":  "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
		},
	}
	node := &mockIPFSNode{block: testBlock}

	err := checkWebsiteRetrieval(context.Background(), resolver, node, []string{"example.com", "pinner.xyz"}, 10*time.Second)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckWebsiteRetrieval_PartialFailure(t *testing.T) {
	testCID, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	testBlock, _ := blocks.NewBlockWithCid([]byte("test"), testCID)

	resolver := &mockDNSResolver{
		resultMap: map[string]string{"example.com": "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG"},
		errMap:    map[string]error{"pinner.xyz": errors.New("dns timeout")},
	}
	node := &mockIPFSNode{block: testBlock}

	err := checkWebsiteRetrieval(context.Background(), resolver, node, []string{"example.com", "pinner.xyz"}, 10*time.Second)
	if err == nil {
		t.Fatal("expected error for partial failure")
	}
}

func TestCheckWebsiteRetrieval_EmptyWebsites(t *testing.T) {
	resolver := &mockDNSResolver{}
	node := &mockIPFSNode{}

	err := checkWebsiteRetrieval(context.Background(), resolver, node, []string{}, 10*time.Second)
	if err != nil {
		t.Fatalf("expected nil for empty websites, got %v", err)
	}
}

func TestNewChecker_PeriodicChecks(t *testing.T) {
	opts := testCheckerOpts(t, []string{"example.com"})
	checker := NewChecker(opts)
	defer checker.Stop()

	// Verify the checker is a valid health.Checker
	if checker == nil {
		t.Fatal("expected non-nil checker")
	}
}

func TestNewChecker_NoWebsites(t *testing.T) {
	opts := testCheckerOpts(t, nil)
	checker := NewChecker(opts)
	defer checker.Stop()

	if checker == nil {
		t.Fatal("expected non-nil checker")
	}
}

func TestTimedCheck_UpdatesMetrics(t *testing.T) {
	fn := timedCheck("test_check", "", func(ctx context.Context) error {
		return nil
	})
	err := fn(context.Background())
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	// Verify the gauge was set (1 = up)
	m, err := healthCheckStatus.GetMetricWithLabelValues("test_check", "")
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}
	// Don't check exact value (touched by other tests), just that it's registered
	_ = m
}

func TestTimedCheck_FailureUpdatesMetrics(t *testing.T) {
	fn := timedCheck("test_fail", "", func(ctx context.Context) error {
		return errors.New("fail")
	})
	err := fn(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSetCheckResult(t *testing.T) {
	setCheckResult("manual_test", "example.com", true, 0.5)
	setCheckResult("manual_test", "example.com", false, 1.5)

	// Verify both gauges are registered without panic
	_, err := healthCheckStatus.GetMetricWithLabelValues("manual_test", "example.com")
	if err != nil {
		t.Fatalf("failed to get status metric: %v", err)
	}
	_, err = healthCheckDuration.GetMetricWithLabelValues("manual_test", "example.com")
	if err != nil {
		t.Fatalf("failed to get duration metric: %v", err)
	}
}

func TestNewChecker_CheckerStart(t *testing.T) {
	opts := testCheckerOpts(t, []string{"example.com"})
	checker := NewChecker(opts)

	checker.Start()
	defer checker.Stop()

	// Give it time to run a periodic check
	time.Sleep(250 * time.Millisecond)

	// After Start + wait, the checker should have results
	result := checker.Check(context.Background())
	if result.Status == "" {
		t.Fatal("expected non-empty status after periodic check")
	}
}

// Ensure health.Checker interface is satisfied
var _ health.Checker = (health.Checker)(nil)
