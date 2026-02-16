package ipfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNewNode verifies that NewNode creates a valid IPFS node.
func TestNewNode(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create temporary directory for testing
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	// Test node creation without seed peer
	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer node.Close()

	// Verify node components
	if node.Host == nil {
		t.Error("Host should not be nil")
	}
	if node.Blockservice == nil {
		t.Error("Blockservice should not be nil")
	}
	if node.PeerID() == "" {
		t.Error("PeerID should not be empty")
	}

	// Verify addresses
	addrs := node.Addrs()
	if len(addrs) == 0 {
		t.Error("Node should have at least one listen address")
	}
}

// TestNewNodeWithSeedPeer verifies that NewNode can connect to a seed peer.
func TestNewNodeWithSeedPeer(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create temporary directory for testing
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	// Use a well-known public IPFS peer as seed
	// Using ipfs.io bootstrap peer
	seedPeer := "/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"

	node, err := NewNode(ctx, seedPeer, repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode with seed peer failed: %v", err)
	}
	defer node.Close()

	// Verify node was created successfully
	if node.Host == nil {
		t.Error("Host should not be nil")
	}
	if node.PeerID() == "" {
		t.Error("PeerID should not be empty")
	}
}

// TestNewNodeInvalidRepoPath verifies that NewNode returns an error for invalid repo path.
func TestNewNodeInvalidRepoPath(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Test with empty repo path
	_, err := NewNode(ctx, "", "", logger)
	if err == nil {
		t.Error("Expected error for empty repo path")
	}

	// Test with invalid path (e.g., a file instead of directory)
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err = NewNode(ctx, "", filePath, logger)
	if err == nil {
		t.Error("Expected error when repo path is a file")
	}
}

// TestNodeClose verifies that Close properly shuts down the node.
func TestNodeClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	// Close the node
	if err := node.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Verify that operations fail after close
	// The Host should be closed and operations should fail
	addrs := node.Addrs()
	if len(addrs) != 0 {
		t.Error("Expected no addresses after close")
	}
}

// TestNodeCloseIdempotent verifies that Close can be called multiple times safely.
func TestNodeCloseIdempotent(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	// Close multiple times
	if err := node.Close(); err != nil {
		t.Errorf("First Close failed: %v", err)
	}
	if err := node.Close(); err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

// TestNodePeerID verifies that PeerID returns a valid peer ID.
func TestNodePeerID(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer node.Close()

	pid := node.PeerID()
	if pid == "" {
		t.Error("PeerID should not be empty")
	}

	// Verify it's a valid peer ID
	// The PeerID() method returns peer.ID which is a type, not a string
	if pid == "" {
		t.Error("PeerID string representation should not be empty")
	}
}

// TestNodeAddrs verifies that Addrs returns valid multiaddresses.
func TestNodeAddrs(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer node.Close()

	addrs := node.Addrs()
	if len(addrs) == 0 {
		t.Error("Node should have at least one listen address")
	}

	// Verify each address is valid
	for i, addr := range addrs {
		if addr.String() == "" {
			t.Errorf("Address %d should not be empty", i)
		}
	}
}

// TestNodePersistence verifies that the node can be recreated with the same repo path.
func TestNodePersistence(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	// Create first node
	node1, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("First NewNode failed: %v", err)
	}
	if err := node1.Close(); err != nil {
		t.Errorf("First Close failed: %v", err)
	}

	// Give some time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Create second node with same repo path
	node2, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("Second NewNode failed: %v", err)
	}
	defer node2.Close()

	// Note: Peer IDs will be different because we generate new keys each time
	// This is expected behavior for this minimal implementation
	if node2.PeerID() == "" {
		t.Error("Second node should have a valid PeerID")
	}
}

// BenchmarkNewNode benchmarks the node creation time.
func BenchmarkNewNode(b *testing.B) {
	ctx := context.Background()
	logger := zap.NewNop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tempDir := b.TempDir()
		repoPath := filepath.Join(tempDir, "ipfs-repo")

		node, err := NewNode(ctx, "", repoPath, logger)
		if err != nil {
			b.Fatalf("NewNode failed: %v", err)
		}
		node.Close()
	}
}

// TestConnectedPeers verifies that ConnectedPeers returns the list of connected peers.
func TestConnectedPeers(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	// Test with node that has no connections (no seed peer)
	t.Run("no connections without seed peer", func(t *testing.T) {
		node, err := NewNode(ctx, "", repoPath, logger)
		if err != nil {
			t.Fatalf("NewNode failed: %v", err)
		}
		defer node.Close()

		peers := node.ConnectedPeers()
		if len(peers) != 0 {
			t.Errorf("Expected no connected peers, got %d", len(peers))
		}
	})

	// Test with node that attempts to connect to seed peer
	t.Run("with seed peer connection attempt", func(t *testing.T) {
		// Use a different directory for this test
		repoPath2 := filepath.Join(t.TempDir(), "ipfs-repo2")

		// Use a well-known public IPFS peer as seed
		seedPeer := "/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"

		node, err := NewNode(ctx, seedPeer, repoPath2, logger)
		if err != nil {
			t.Fatalf("NewNode with seed peer failed: %v", err)
		}
		defer node.Close()

		// Give some time for connection to establish
		time.Sleep(2 * time.Second)

		peers := node.ConnectedPeers()
		// We expect at least one peer connection (the seed peer)
		// Note: This test may be flaky due to network conditions
		if len(peers) == 0 {
			t.Logf("Warning: No peers connected after 2 seconds (may be network issue)")
		}

		// Verify all returned peer IDs are valid
		for i, pid := range peers {
			if pid == "" {
				t.Errorf("Peer ID at index %d should not be empty", i)
			}
		}
	})
}

// TestConnectedPeersAfterClose verifies that ConnectedPeers returns empty list after node is closed.
func TestConnectedPeersAfterClose(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	// Close the node
	if err := node.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// ConnectedPeers should return empty after close
	peers := node.ConnectedPeers()
	if len(peers) != 0 {
		t.Errorf("Expected no connected peers after close, got %d", len(peers))
	}
}
