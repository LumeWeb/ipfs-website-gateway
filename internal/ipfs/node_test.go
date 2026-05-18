package ipfs

import (
	"context"
	"testing"

	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
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

	node, err := NewNode(ctx, "", bs, logger)
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

	node, err := NewNode(ctx, seedPeer, bs, logger)
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

	_, err := NewNode(ctx, "", nil, logger)
	if err == nil {
		t.Error("Expected error for nil blockstore")
	}
}

func TestNodeClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	bs := newTestBlockstore(t)

	node, err := NewNode(ctx, "", bs, logger)
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

	node, err := NewNode(ctx, "", bs, logger)
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

	node, err := NewNode(ctx, "", bs, logger)
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

	node, err := NewNode(ctx, "", bs, logger)
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

	node, err := NewNode(ctx, "", bs, logger)
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

	node, err := NewNode(ctx, "", bs, logger)
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

		node, err := NewNode(ctx, "", bs, logger)
		if err != nil {
			b.Fatalf("NewNode failed: %v", err)
		}
		_ = node.Close()
	}
}
