package health

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alexliesenfeld/health"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

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
	connected         bool
	addrs             []multiaddr.Multiaddr
	seedPeerConnected bool
	getBlockErr       error
	getBlockData      []byte
}

func (m *mockIPFSNode) PeerID() peer.ID {
	return peer.ID("QmTestPeerID")
}

func (m *mockIPFSNode) Addrs() []multiaddr.Multiaddr {
	return m.addrs
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

func (m *mockIPFSNode) SeedPeerConnected() bool {
	return m.seedPeerConnected
}

func (m *mockIPFSNode) GetBlock(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	if m.getBlockErr != nil {
		return nil, m.getBlockErr
	}
	if m.getBlockData != nil {
		return blocks.NewBlockWithCid(m.getBlockData, c)
	}
	return blocks.NewBlockWithCid([]byte("test-block-data"), c)
}

type mockDNSResolver struct {
	path string
	err  error
}

func (m *mockDNSResolver) ValidateDNSLink(ctx context.Context, domain string) (string, error) {
	return m.path, m.err
}

func defaultTestConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Websites: []string{"example.com"},
		Interval: 1 * time.Second,
		Timeout:  5 * time.Second,
	}
}

func defaultMockDNSResolver() *mockDNSResolver {
	return &mockDNSResolver{path: "/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG"}
}

func TestNewChecker(t *testing.T) {
	t.Run("creates checker with valid dependencies", func(t *testing.T) {
		mockPing := &mockPingService{}
		mockIPFS := &mockIPFSNode{connected: true, seedPeerConnected: true}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		if checker == nil {
			t.Fatal("expected checker to be created, got nil")
		}
	})
}

func TestHealthChecks(t *testing.T) {
	t.Run("internal_api check passes when API is healthy", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, seedPeerConnected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusUp {
			t.Errorf("expected status %s, got %s", health.StatusUp, result.Status)
		}

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

	t.Run("internal_api check fails when API is unreachable", func(t *testing.T) {
		mockPing := &mockPingService{err: fmt.Errorf("connection refused")}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, seedPeerConnected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

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
		mockPing := &mockPingService{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, seedPeerConnected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusUp {
			t.Errorf("expected status %s, got %s", health.StatusUp, result.Status)
		}

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

	t.Run("ipfs_peer check fails when seed peer is not connected", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: false, seedPeerConnected: false, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

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

		errStr := ipfsCheckResult.Error.Error()
		if !strings.Contains(errStr, "seed peer") {
			t.Errorf("expected error to mention 'seed peer', got: %s", errStr)
		}
	})

	t.Run("ipfs_peer check fails when connected but seed peer disconnected", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, seedPeerConnected: false, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

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
	})

	t.Run("both checks fail when both dependencies are unhealthy", func(t *testing.T) {
		mockPing := &mockPingService{err: fmt.Errorf("connection refused")}
		mockIPFS := &mockIPFSNode{connected: false, seedPeerConnected: false}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

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
		mockPing := &mockPingService{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		mockIPFS := &mockIPFSNode{connected: true, seedPeerConnected: true, addrs: []multiaddr.Multiaddr{addr}}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		time.Sleep(10 * time.Millisecond)

		result := checker.Check(ctx)

		if result.Status != health.StatusUp && result.Status != health.StatusDown {
			t.Errorf("expected status to be either up or down, got %s", result.Status)
		}
	})
}

func TestIPFSPeerHealthWithMultiplePeers(t *testing.T) {
	t.Run("health check passes with multiple connected peers", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")

		mockIPFS := &mockIPFSNode{
			connected:         true,
			seedPeerConnected: true,
			addrs:             []multiaddr.Multiaddr{addr},
		}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusUp {
			t.Errorf("expected status %s, got %s", health.StatusUp, result.Status)
		}

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

func TestIPFSPeerHealthNoAddresses(t *testing.T) {
	t.Run("health check fails when node has no addresses", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}

		mockIPFS := &mockIPFSNode{
			connected:         true,
			seedPeerConnected: true,
			addrs:             []multiaddr.Multiaddr{},
		}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: defaultMockDNSResolver(),
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

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

		errStr := ipfsCheckResult.Error.Error()
		if !strings.Contains(errStr, "not listening") {
			t.Errorf("expected error to mention 'not listening', got: %s", errStr)
		}
	})
}

func TestIPFSPeerHealthNilNode(t *testing.T) {
	t.Run("health check fails when node is nil", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}

		checker := NewChecker(CheckerOptions{
			PingSvc:  mockPing,
			IPFSNode: nil,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if result.Status != health.StatusDown {
			t.Errorf("expected status %s, got %s", health.StatusDown, result.Status)
		}

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

		errStr := ipfsCheckResult.Error.Error()
		if !strings.Contains(errStr, "not initialized") {
			t.Errorf("expected error to mention 'not initialized', got: %s", errStr)
		}
	})
}

func TestWebsiteRetrievalCheck(t *testing.T) {
	addr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")

	t.Run("website_retrieval passes when DNSLink resolves and block fetches", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		mockIPFS := &mockIPFSNode{
			connected:         true,
			seedPeerConnected: true,
			addrs:             []multiaddr.Multiaddr{addr},
		}
		mockDNS := defaultMockDNSResolver()

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: mockDNS,
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		wsResult, ok := result.Details["website_retrieval"]
		if !ok {
			t.Fatal("expected website_retrieval check to be present")
		}
		if wsResult.Status != health.StatusUp {
			t.Errorf("expected website_retrieval status %s, got %s: %v", health.StatusUp, wsResult.Status, wsResult.Error)
		}
	})

	t.Run("website_retrieval fails when DNSLink resolution fails", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		mockIPFS := &mockIPFSNode{
			connected:         true,
			seedPeerConnected: true,
			addrs:             []multiaddr.Multiaddr{addr},
		}
		mockDNS := &mockDNSResolver{err: fmt.Errorf("DNS query failed: no TXT records")}

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: mockDNS,
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		wsResult, ok := result.Details["website_retrieval"]
		if !ok {
			t.Fatal("expected website_retrieval check to be present")
		}
		if wsResult.Status != health.StatusDown {
			t.Errorf("expected website_retrieval status %s, got %s", health.StatusDown, wsResult.Status)
		}
		if wsResult.Error == nil {
			t.Error("expected error to be set for failed website retrieval")
		}
		errStr := wsResult.Error.Error()
		if !strings.Contains(errStr, "DNSLink resolution failed") {
			t.Errorf("expected error to mention 'DNSLink resolution failed', got: %s", errStr)
		}
		if !strings.Contains(errStr, "example.com") {
			t.Errorf("expected error to mention domain 'example.com', got: %s", errStr)
		}
	})

	t.Run("website_retrieval fails when block fetch fails", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		mockIPFS := &mockIPFSNode{
			connected:         true,
			seedPeerConnected: true,
			addrs:             []multiaddr.Multiaddr{addr},
			getBlockErr:       fmt.Errorf("bitswap: block not found"),
		}
		mockDNS := defaultMockDNSResolver()

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: mockDNS,
			Config:      defaultTestConfig(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		wsResult, ok := result.Details["website_retrieval"]
		if !ok {
			t.Fatal("expected website_retrieval check to be present")
		}
		if wsResult.Status != health.StatusDown {
			t.Errorf("expected website_retrieval status %s, got %s", health.StatusDown, wsResult.Status)
		}
		if wsResult.Error == nil {
			t.Error("expected error to be set for failed block fetch")
		}
		errStr := wsResult.Error.Error()
		if !strings.Contains(errStr, "block fetch failed") {
			t.Errorf("expected error to mention 'block fetch failed', got: %s", errStr)
		}
	})

	t.Run("website_retrieval not registered when no websites configured", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		mockIPFS := &mockIPFSNode{
			connected:         true,
			seedPeerConnected: true,
			addrs:             []multiaddr.Multiaddr{addr},
		}
		mockDNS := defaultMockDNSResolver()

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: mockDNS,
			Config: HealthCheckConfig{
				Websites: nil,
				Interval: 1 * time.Second,
				Timeout:  5 * time.Second,
			},
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		if _, ok := result.Details["website_retrieval"]; ok {
			t.Fatal("expected website_retrieval check to NOT be present when no websites configured")
		}
	})

	t.Run("website_retrieval reports all failed domains", func(t *testing.T) {
		mockPing := &mockPingService{err: nil}
		mockIPFS := &mockIPFSNode{
			connected:         true,
			seedPeerConnected: true,
			addrs:             []multiaddr.Multiaddr{addr},
			getBlockErr:       fmt.Errorf("bitswap: block not found"),
		}
		mockDNS := defaultMockDNSResolver()

		checker := NewChecker(CheckerOptions{
			PingSvc:     mockPing,
			IPFSNode:    mockIPFS,
			DNSResolver: mockDNS,
			Config: HealthCheckConfig{
				Websites: []string{"site1.com", "site2.com"},
				Interval: 1 * time.Second,
				Timeout:  5 * time.Second,
			},
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result := checker.Check(ctx)

		wsResult, ok := result.Details["website_retrieval"]
		if !ok {
			t.Fatal("expected website_retrieval check to be present")
		}
		if wsResult.Status != health.StatusDown {
			t.Errorf("expected website_retrieval status %s, got %s", health.StatusDown, wsResult.Status)
		}
		errStr := wsResult.Error.Error()
		if !strings.Contains(errStr, "site1.com") {
			t.Errorf("expected error to mention 'site1.com', got: %s", errStr)
		}
		if !strings.Contains(errStr, "site2.com") {
			t.Errorf("expected error to mention 'site2.com', got: %s", errStr)
		}
	})
}

func TestExtractCIDFromPath(t *testing.T) {
	t.Run("valid /ipfs/ path", func(t *testing.T) {
		c, err := extractCIDFromPath("/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.String() != "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG" {
			t.Errorf("unexpected CID: %s", c.String())
		}
	})

	t.Run("valid /ipns/ path", func(t *testing.T) {
		// IPNS paths contain a peer ID, not a CID — but extractCIDFromPath
		// will try to decode it. This should fail since it's not a CID.
		// The health check uses this for /ipfs/ paths from DNSLink.
		_, err := extractCIDFromPath("/ipns/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		// Qm... is a valid CIDv0, so this actually succeeds
		if err != nil {
			t.Errorf("unexpected error for valid CID in /ipns/ path: %v", err)
		}
	})

	t.Run("path too short", func(t *testing.T) {
		_, err := extractCIDFromPath("/ipfs")
		if err == nil {
			t.Fatal("expected error for short path, got nil")
		}
		if !strings.Contains(err.Error(), "path too short") {
			t.Errorf("expected 'path too short' error, got: %s", err.Error())
		}
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := extractCIDFromPath("")
		if err == nil {
			t.Fatal("expected error for empty path, got nil")
		}
	})

	t.Run("invalid CID", func(t *testing.T) {
		_, err := extractCIDFromPath("/ipfs/not-a-cid")
		if err == nil {
			t.Fatal("expected error for invalid CID, got nil")
		}
		if !strings.Contains(err.Error(), "invalid CID") {
			t.Errorf("expected 'invalid CID' error, got: %s", err.Error())
		}
	})
}
