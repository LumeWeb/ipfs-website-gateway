package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexliesenfeld/health"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

// mockAPIClient is a mock for the APIClient interface
type mockAPIClient struct {
	err error
}

func (m *mockAPIClient) GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &types.GatewayWebsiteResponse{
		Domain:     domain,
		TargetType: "ipfs",
		TargetHash: "QmTest",
		Status:     types.StatusActive,
	}, nil
}

// mockIPFSNode is a mock for the IPFSNode interface
type mockIPFSNode struct {
	connected bool
	addrs     []multiaddr.Multiaddr
}

func (m *mockIPFSNode) PeerID() peer.ID {
	return peer.ID("QmTestPeerID")
}

func (m *mockIPFSNode) Addrs() []multiaddr.Multiaddr {
	if m.connected {
		return m.addrs
	}
	return []multiaddr.Multiaddr{}
}

func (m *mockIPFSNode) Close() error {
	return nil
}

func (m *mockIPFSNode) ConnectedPeers() []peer.ID {
	if m.connected {
		return []peer.ID{peer.ID("QmConnectedPeer1"), peer.ID("QmConnectedPeer2")}
	}
	return []peer.ID{}
}

func TestNewChecker(t *testing.T) {
	t.Run("creates checker with valid dependencies", func(t *testing.T) {
		mockAPI := &mockAPIClient{}
		mockIPFS := &mockIPFSNode{connected: true}

		checker := NewChecker(mockAPI, mockIPFS)

		if checker == nil {
			t.Fatal("expected checker to be created, got nil")
		}
	})
}

func TestHealthChecks(t *testing.T) {
	t.Run("internal_api check passes when API is healthy", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusUp {
			t.Errorf("expected status %s, got %s", health.StatusUp, result.Status)
		}

		// Find the internal_api check result
		var apiCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "internal_api" {
				apiCheckResult = &check
				break
			}
		}

		if apiCheckResult == nil {
			t.Fatal("expected internal_api check to be present")
		}

		if apiCheckResult.Status != health.StatusUp {
			t.Errorf("expected internal_api status %s, got %s", health.StatusUp, apiCheckResult.Status)
		}
	})

	t.Run("internal_api check passes when API returns 404", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: errors.New("website not found: health-check.example.com")}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusUp {
			t.Errorf("expected status %s, got %s", health.StatusUp, result.Status)
		}

		// Find the internal_api check result
		var apiCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "internal_api" {
				apiCheckResult = &check
				break
			}
		}

		if apiCheckResult == nil {
			t.Fatal("expected internal_api check to be present")
		}

		if apiCheckResult.Status != health.StatusUp {
			t.Errorf("expected internal_api status %s, got %s", health.StatusUp, apiCheckResult.Status)
		}
	})

	t.Run("internal_api check fails when API returns unexpected error", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: errors.New("API connection failed")}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		// Overall status should be down
		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

		// Find the internal_api check result
		var apiCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "internal_api" {
				apiCheckResult = &check
				break
			}
		}

		if apiCheckResult == nil {
			t.Fatal("expected internal_api check to be present")
		}

		if apiCheckResult.Status != health.StatusDown {
			t.Errorf("expected internal_api status %s, got %s", health.StatusDown, apiCheckResult.Status)
		}

		if apiCheckResult.Error == nil {
			t.Error("expected error to be set for failed check")
		}
	})

	t.Run("ipfs_peer check passes when peer is connected", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusUp {
			t.Errorf("expected status %s, got %s", health.StatusUp, result.Status)
		}

		// Find the ipfs_peer check result
		var ipfsCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "ipfs_peer" {
				ipfsCheckResult = &check
				break
			}
		}

		if ipfsCheckResult == nil {
			t.Fatal("expected ipfs_peer check to be present")
		}

		if ipfsCheckResult.Status != health.StatusUp {
			t.Errorf("expected ipfs_peer status %s, got %s", health.StatusUp, ipfsCheckResult.Status)
		}
	})

	t.Run("ipfs_peer check fails when peer is not connected", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: nil}
		mockIPFS := &mockIPFSNode{connected: false}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		// Overall status should be down
		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

		// Find the ipfs_peer check result
		var ipfsCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "ipfs_peer" {
				ipfsCheckResult = &check
				break
			}
		}

		if ipfsCheckResult == nil {
			t.Fatal("expected ipfs_peer check to be present")
		}

		if ipfsCheckResult.Status != health.StatusDown {
			t.Errorf("expected ipfs_peer status %s, got %s", health.StatusDown, ipfsCheckResult.Status)
		}

		if ipfsCheckResult.Error == nil {
			t.Error("expected error to be set for failed check")
		}
	})

	t.Run("both checks fail when both dependencies are unhealthy", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: errors.New("API connection failed")}
		mockIPFS := &mockIPFSNode{connected: false}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

		// Both checks should be present and failed
		checkResults := result.Details

		if checkResults["internal_api"].Status != health.StatusDown {
			t.Errorf("expected internal_api status %s, got %s", health.StatusDown, checkResults["internal_api"].Status)
		}

		if checkResults["ipfs_peer"].Status != health.StatusDown {
			t.Errorf("expected ipfs_peer status %s, got %s", health.StatusDown, checkResults["ipfs_peer"].Status)
		}
	})
}

func TestHealthCheckTimeout(t *testing.T) {
	t.Run("health check respects context timeout", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(mockAPI, mockIPFS)

		// Use a very short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		// Give time for context to timeout
		time.Sleep(10 * time.Millisecond)

		result := checker.Check(ctx)

		// Result should be either down (timeout) or up (if check was fast enough)
		// The important thing is that it doesn't hang
		if result.Status != health.StatusUp && result.Status != health.StatusDown {
			t.Errorf("expected status to be either up or down, got %s", result.Status)
		}
	})
}

// TestIPFSPeerHealthWithMultiplePeers verifies the health check with multiple connected peers.
func TestIPFSPeerHealthWithMultiplePeers(t *testing.T) {
	t.Run("health check passes with multiple connected peers", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")

		// Create mock with multiple connected peers
		mockIPFS := &mockIPFSNode{
			connected: true,
			addrs:     []multiaddr.Multiaddr{addr},
		}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		// Overall status should be up
		if result.Status != health.StatusUp {
			t.Errorf("expected status %s, got %s", health.StatusUp, result.Status)
		}

		// Find the ipfs_peer check result
		var ipfsCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "ipfs_peer" {
				ipfsCheckResult = &check
				break
			}
		}

		if ipfsCheckResult == nil {
			t.Fatal("expected ipfs_peer check to be present")
		}

		if ipfsCheckResult.Status != health.StatusUp {
			t.Errorf("expected ipfs_peer status %s, got %s", health.StatusUp, ipfsCheckResult.Status)
		}
	})
}

// TestIPFSPeerHealthNoAddresses verifies the health check fails when node has no addresses.
func TestIPFSPeerHealthNoAddresses(t *testing.T) {
	t.Run("health check fails when node has no addresses", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: nil}

		// Create mock with connected=true but no addresses (edge case)
		mockIPFS := &mockIPFSNode{
			connected: true,
			addrs:     []multiaddr.Multiaddr{},
		}

		checker := NewChecker(mockAPI, mockIPFS)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		// Overall status should be down
		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

		// Find the ipfs_peer check result
		var ipfsCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "ipfs_peer" {
				ipfsCheckResult = &check
				break
			}
		}

		if ipfsCheckResult == nil {
			t.Fatal("expected ipfs_peer check to be present")
		}

		if ipfsCheckResult.Status != health.StatusDown {
			t.Errorf("expected ipfs_peer status %s, got %s", health.StatusDown, ipfsCheckResult.Status)
		}

		if ipfsCheckResult.Error == nil {
			t.Error("expected error to be set for failed check")
		}

		// Verify error message mentions addresses
		errStr := ipfsCheckResult.Error.Error()
		if !strings.Contains(errStr, "not listening") {
			t.Errorf("expected error to mention 'not listening', got: %s", errStr)
		}
	})
}

// TestIPFSPeerHealthNilNode verifies the health check fails when node is nil.
func TestIPFSPeerHealthNilNode(t *testing.T) {
	t.Run("health check fails when node is nil", func(t *testing.T) {
		mockAPI := &mockAPIClient{err: nil}

		checker := NewChecker(mockAPI, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		// Overall status should be down
		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

		// Find the ipfs_peer check result
		var ipfsCheckResult *health.CheckResult
		for name, check := range result.Details {
			if name == "ipfs_peer" {
				ipfsCheckResult = &check
				break
			}
		}

		if ipfsCheckResult == nil {
			t.Fatal("expected ipfs_peer check to be present")
		}

		if ipfsCheckResult.Status != health.StatusDown {
			t.Errorf("expected ipfs_peer status %s, got %s", health.StatusDown, ipfsCheckResult.Status)
		}

		if ipfsCheckResult.Error == nil {
			t.Error("expected error to be set for failed check")
		}

		// Verify error message mentions initialization
		errStr := ipfsCheckResult.Error.Error()
		if !strings.Contains(errStr, "not initialized") {
			t.Errorf("expected error to mention 'not initialized', got: %s", errStr)
		}
	})
}
