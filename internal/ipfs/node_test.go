package ipfs

import (
	"context"
	"testing"
	"time"

	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	routinghelpers "github.com/libp2p/go-libp2p-routing-helpers"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

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

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
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

func TestNewNodeWithSeedPeer(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	seedPeer := "/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"

	node, err := NewNode(ctx, seedPeer, 30*time.Second, bs, logger)
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

	_, err := NewNode(ctx, "", 30*time.Second, nil, logger)
	if err == nil {
		t.Error("Expected error for nil blockstore")
	}
}

func TestNodeClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
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

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
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

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
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

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
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

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
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

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
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

func TestNewNodeWithSeedPeerRoutingSet(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	seedPeer := "/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"

	node, err := NewNode(ctx, seedPeer, 30*time.Second, bs, logger)
	if err != nil {
		t.Fatalf("NewNode with seed peer failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	if node.Routing == nil {
		t.Error("Routing should not be nil")
	}

	_, isNull := node.routing.current.(routinghelpers.Null)
	if isNull {
		t.Error("Routing should not be Null when seed peer connects successfully")
	}

	node.seedPeerRetry.mu.Lock()
	retryRunning := node.seedPeerRetry.running
	node.seedPeerRetry.mu.Unlock()
	if retryRunning {
		t.Error("Seed peer retry should not be running when seed peer connected successfully")
	}
}

func TestNewNodeWithUnreachableSeedPeer(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	seedPeer := "/ip4/192.0.2.1/tcp/4001/p2p/12D3KooWRBYMhRRPFnEasFnLiSnEC8YWFC5wpFcFfb6V3V33Wmqr"

	node, err := NewNode(ctx, seedPeer, 1*time.Second, bs, logger)
	if err != nil {
		t.Fatalf("NewNode should not fail with unreachable seed peer: %v", err)
	}
	defer func() { _ = node.Close() }()

	_, isNull := node.routing.current.(routinghelpers.Null)
	if !isNull {
		t.Error("Routing should be Null when seed peer connection fails")
	}

	node.seedPeerRetry.mu.Lock()
	retryRunning := node.seedPeerRetry.running
	retryPeer := node.seedPeerRetry.peer
	node.seedPeerRetry.mu.Unlock()

	if !retryRunning {
		t.Error("Seed peer retry should be running when seed peer connection fails")
	}
	if retryPeer != seedPeer {
		t.Errorf("Retry peer = %q, want %q", retryPeer, seedPeer)
	}
}

func TestNewNodeNoSeedPeerNoRetry(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	_, isNull := node.routing.current.(routinghelpers.Null)
	if !isNull {
		t.Error("Routing should be Null when no seed peer is configured")
	}

	node.seedPeerRetry.mu.Lock()
	retryRunning := node.seedPeerRetry.running
	node.seedPeerRetry.mu.Unlock()

	if retryRunning {
		t.Error("Seed peer retry should not be running when no seed peer is configured")
	}
}

func TestSeedPeerRetryStopsOnClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	seedPeer := "/ip4/192.0.2.1/tcp/4001/p2p/12D3KooWRBYMhRRPFnEasFnLiSnEC8YWFC5wpFcFfb6V3V33Wmqr"

	node, err := NewNode(ctx, seedPeer, 1*time.Second, bs, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	node.seedPeerRetry.mu.Lock()
	wasRunning := node.seedPeerRetry.running
	stopCh := node.seedPeerRetry.stopCh
	node.seedPeerRetry.mu.Unlock()

	if !wasRunning {
		t.Fatal("Expected retry to be running before close")
	}

	if err := node.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	select {
	case <-stopCh:
	default:
		t.Error("Stop channel should be closed after node.Close()")
	}

	node.seedPeerRetry.mu.Lock()
	stillRunning := node.seedPeerRetry.running
	node.seedPeerRetry.mu.Unlock()

	if stillRunning {
		t.Error("Seed peer retry should not be running after close")
	}
}

func TestSeedPeerRetrySucceeds(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	seedPeer := "/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"

	node, err := NewNode(ctx, seedPeer, 30*time.Second, bs, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	_, isNull := node.routing.current.(routinghelpers.Null)
	if isNull {
		t.Error("Routing should not be Null after successful seed peer connection")
	}

	select {
	case <-node.ctx.Done():
		t.Error("Node context should not be done")
	default:
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

		node, err := NewNode(ctx, "", 30*time.Second, bs, logger)
		if err != nil {
			b.Fatalf("NewNode failed: %v", err)
		}
		_ = node.Close()
	}
}
