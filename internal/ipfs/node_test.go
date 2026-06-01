package ipfs

import (
	"context"
	"testing"
	"time"

	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// WARNING: This is a TEST seed for CI/CD purposes only.
// This seed is INTENTIONALLY COMPROMISED and MUST NOT be used in production.
// DO NOT use this seed for any real deployments or secure operations.
const testSeed = "victory coin oven horn blade sausage large jungle differ talent coral jewel"

func newTestBlockstore(t *testing.T) *cache.ContentBlockstore {
	t.Helper()
	tmpDir := t.TempDir()
	cc, err := cache.NewContentCache(tmpDir, 10*1024*1024, 1000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}
	return cache.NewContentBlockstore(cc, zap.NewNop())
}

func TestNewNode(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	if node.Host == nil {
		t.Error("Host should not be nil")
	}
	if node.BlockService == nil {
		t.Error("BlockService should not be nil")
	}
	if node.PeerID() == "" {
		t.Error("PeerID should not be empty")
	}

	addrs := node.Addrs()
	if len(addrs) == 0 {
		t.Error("Node should have at least one listen address")
	}
}

func TestNewNodeWithRoutingEndpoint(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "https://api.pinner.xyz/routing/v1", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode with routing endpoint failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	if node.Routing == nil {
		t.Error("Routing should not be nil when routing endpoint is configured")
	}
	if node.routingClient == nil {
		t.Error("routingClient should not be nil when routing endpoint is configured")
	}
}

func TestNewNodeWithEmptyRoutingEndpoint(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	if node.Routing != nil {
		t.Error("Routing should be nil when no routing endpoint is configured")
	}
}

func TestNewNodeWithSeedPeer(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	seedPeer := "/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"

	node, err := NewNode(ctx, seedPeer, 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode with seed peer failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	if node.Host == nil {
		t.Error("Host should not be nil")
	}
	if node.PeerID() == "" {
		t.Error("PeerID should not be empty")
	}
}

func TestNewNodeNilBlockstore(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	_, err := NewNode(ctx, "", 30*time.Second, "", nil, logger, false, testSeed)
	if err == nil {
		t.Error("Expected error for nil blockstore")
	}
}

func TestNodeClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	if err := node.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	addrs := node.Addrs()
	if len(addrs) != 0 {
		t.Error("Expected no addresses after close")
	}
}

func TestNodeCloseIdempotent(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	if err := node.Close(); err != nil {
		t.Errorf("First Close failed: %v", err)
	}
	if err := node.Close(); err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestNodePeerID(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	pid := node.PeerID()
	if pid == "" {
		t.Error("PeerID should not be empty")
	}
}

func TestNodeAddrs(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	addrs := node.Addrs()
	if len(addrs) == 0 {
		t.Error("Node should have at least one listen address")
	}

	for i, addr := range addrs {
		if addr.String() == "" {
			t.Errorf("Address %d should not be empty", i)
		}
	}
}

func TestConnectedPeers(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	peers := node.ConnectedPeers()
	if len(peers) != 0 {
		t.Errorf("Expected no connected peers, got %d", len(peers))
	}
}

func TestConnectedPeersAfterClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	if err := node.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	peers := node.ConnectedPeers()
	if len(peers) != 0 {
		t.Errorf("Expected no connected peers after close, got %d", len(peers))
	}
}

func TestNewNodeWithPubsubAndRouting(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "https://api.pinner.xyz/routing/v1", bs, logger, true, testSeed)
	if err != nil {
		t.Fatalf("NewNode with pubsub and routing failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	if node.Routing == nil {
		t.Error("Routing should not be nil with routing endpoint configured")
	}
}

func TestConnectSeedPeerNoAddr(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	node.seedPeerAddr = ""
	node.ConnectSeedPeer()

	node.seedPeerMu.Lock()
	active := node.seedPeerActive
	node.seedPeerMu.Unlock()
	if active {
		t.Error("ConnectSeedPeer should not start worker when seed peer addr is empty")
	}
}

func TestConnectSeedPeerIdempotent(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	node.seedPeerAddr = "/dnsaddr/ipfs.pinner.xyz"
	node.seedPeerConnectTimeout = 1 * time.Second
	node.ConnectSeedPeer()
	node.ConnectSeedPeer()

	time.Sleep(50 * time.Millisecond)

	node.seedPeerMu.Lock()
	active := node.seedPeerActive
	cancel := node.seedPeerCancel
	node.seedPeerMu.Unlock()

	if !active {
		t.Error("expected worker to be active after ConnectSeedPeer")
	}
	if cancel == nil {
		t.Error("expected cancel func to be set")
	}

	node.DisconnectSeedPeer()

	node.seedPeerMu.Lock()
	active = node.seedPeerActive
	node.seedPeerMu.Unlock()

	if active {
		t.Error("expected worker to be inactive after DisconnectSeedPeer")
	}
}

func TestDisconnectSeedPeerIdempotent(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	node.DisconnectSeedPeer()
	node.DisconnectSeedPeer()
}

func TestSeedPeerWorkerStopsOnClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	node.seedPeerAddr = "/dnsaddr/ipfs.pinner.xyz"
	node.seedPeerConnectTimeout = 1 * time.Second
	node.ConnectSeedPeer()

	time.Sleep(50 * time.Millisecond)

	node.seedPeerMu.Lock()
	activeBefore := node.seedPeerActive
	node.seedPeerMu.Unlock()

	if !activeBefore {
		t.Error("expected worker to be active before Close")
	}

	_ = node.Close()

	node.seedPeerMu.Lock()
	activeAfter := node.seedPeerActive
	node.seedPeerMu.Unlock()

	if activeAfter {
		t.Error("expected worker to be inactive after Close")
	}
}

func TestNewNodeWithUnreachableSeedPeerUnblocks(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	start := time.Now()
	node, err := NewNode(ctx, "/dnsaddr/nonexistent.invalid/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN", 1*time.Second, "", bs, logger, false, testSeed)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("NewNode should not fail on unreachable seed peer: %v", err)
	}
	defer func() { _ = node.Close() }()

	if elapsed > 5*time.Second {
		t.Errorf("NewNode blocked too long (%v) — seed peer connect should not block boot", elapsed)
	}

	node.seedPeerMu.Lock()
	active := node.seedPeerActive
	node.seedPeerMu.Unlock()

	if !active {
		t.Error("expected reconnection worker to be active for unreachable seed peer")
	}
}

func BenchmarkNewNode(b *testing.B) {
	ctx := context.Background()
	logger := zap.NewNop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tmpDir := b.TempDir()
		cc, err := cache.NewContentCache(tmpDir, 10*1024*1024, 1000)
		if err != nil {
			b.Fatalf("NewContentCache: %v", err)
		}
		bs := cache.NewContentBlockstore(cc, zap.NewNop())

		node, err := NewNode(ctx, "", 30*time.Second, "", bs, logger, false, testSeed)
		if err != nil {
			b.Fatalf("NewNode failed: %v", err)
		}
		_ = node.Close()
	}
}
