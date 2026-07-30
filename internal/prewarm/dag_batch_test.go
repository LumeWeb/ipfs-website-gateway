package prewarm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ipfs/boxo/blockservice"
	blocks "github.com/ipfs/go-block-format"
	format "github.com/ipfs/go-ipld-format"
	"go.uber.org/zap"
)

// mockDAGResolver is a configurable mock DAGResolver for testing.
type mockDAGResolver struct {
	mu       sync.Mutex
	called   int
	respond  func(cidStr string) ([]DAGNode, error)
	callCount int
}

func (m *mockDAGResolver) ResolveDAG(ctx context.Context, cidStr string) ([]DAGNode, error) {
	m.mu.Lock()
	m.called++
	m.mu.Unlock()
	if m.respond != nil {
		return m.respond(cidStr)
	}
	return nil, nil
}

func newTestPrewarmerWithDAG(t *testing.T, bs blockservice.BlockService, dagResolver DAGResolver) *Prewarmer {
	t.Helper()
	p, err := NewPrewarmer(context.Background(), bs, dagResolver, zap.NewNop(), 30*time.Second, 2, 2, 1*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestWalkBatch_FetchesAllBlocks verifies that walkBatch calls the DAG
// resolver, gets the node list, and fetches all blocks from the blockservice.
func TestWalkBatch_FetchesAllBlocks(t *testing.T) {
	bs := newTestBlockService(t)

	// Create 3 raw blocks
	data1 := []byte("block1")
	data2 := []byte("block2")
	data3 := []byte("block3")
	blk1 := makeRawBlock(t, data1)
	blk2 := makeRawBlock(t, data2)
	blk3 := makeRawBlock(t, data3)

	// Put them in the blockstore so Get succeeds
	ctx := context.Background()
	if err := bs.Blockstore().Put(ctx, blk1); err != nil {
		t.Fatal(err)
	}
	if err := bs.Blockstore().Put(ctx, blk2); err != nil {
		t.Fatal(err)
	}
	if err := bs.Blockstore().Put(ctx, blk3); err != nil {
		t.Fatal(err)
	}

	// Mock resolver returns all 3 blocks as a flat DAG
	resolver := &mockDAGResolver{
		respond: func(rootCid string) ([]DAGNode, error) {
			return []DAGNode{
				{CID: blk1.Cid().String(), Size: len(data1), Children: []string{blk2.Cid().String(), blk3.Cid().String()}},
				{CID: blk2.Cid().String(), Size: len(data2), Children: nil},
				{CID: blk3.Cid().String(), Size: len(data3), Children: nil},
			}, nil
		},
	}

	p := newTestPrewarmerWithDAG(t, bs, resolver)
	defer p.Stop()

	visited, skipped, elapsed, err := p.walkBatch(ctx, blk1.Cid())
	if err != nil {
		t.Fatalf("walkBatch failed: %v", err)
	}
	if visited != 3 {
		t.Fatalf("expected 3 visited, got %d", visited)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", skipped)
	}
	if elapsed <= 0 {
		t.Fatal("expected positive elapsed time")
	}

	if resolver.called != 1 {
		t.Fatalf("expected resolver called once, got %d", resolver.called)
	}
}

// TestWalkBatch_EmptyResult verifies walkBatch returns 0 visited
// when the resolver returns no nodes.
func TestWalkBatch_EmptyResult(t *testing.T) {
	bs := newTestBlockService(t)
	resolver := &mockDAGResolver{
		respond: func(rootCid string) ([]DAGNode, error) {
			return nil, nil
		},
	}

	p := newTestPrewarmerWithDAG(t, bs, resolver)
	defer p.Stop()

	c := makeRawCID([]byte("root"))
	visited, _, _, err := p.walkBatch(context.Background(), c)
	if err != nil {
		t.Fatalf("walkBatch failed: %v", err)
	}
	if visited != 0 {
		t.Fatalf("expected 0 visited for empty DAG, got %d", visited)
	}
}

// TestWalkBatch_ResolverError verifies walkBatch returns the error
// when the resolver fails.
func TestWalkBatch_ResolverError(t *testing.T) {
	bs := newTestBlockService(t)
	resolver := &mockDAGResolver{
		respond: func(rootCid string) ([]DAGNode, error) {
			return nil, fmt.Errorf("portal unreachable")
		},
	}

	p := newTestPrewarmerWithDAG(t, bs, resolver)
	defer p.Stop()

	c := makeRawCID([]byte("root"))
	visited, _, _, err := p.walkBatch(context.Background(), c)
	if err == nil {
		t.Fatal("expected error from resolver failure")
	}
	if visited != 0 {
		t.Fatalf("expected 0 visited on error, got %d", visited)
	}
}

// TestPrewarm_FallbackToSequential verifies that when the DAG resolver
// fails, prewarm falls back to the sequential merkledag walk.
func TestPrewarm_FallbackToSequential(t *testing.T) {
	bs := newTestBlockService(t)

	// Create a simple DAG: root -> child
	childData := []byte("child")
	childBlock := makeRawBlock(t, childData)
	rootNode := makeDirNode(t, &format.Link{
		Cid:  childBlock.Cid(),
		Name: "child",
		Size: uint64(len(childData)),
	})

	ctx := context.Background()
	if err := bs.Blockstore().Put(ctx, childBlock); err != nil {
		t.Fatal(err)
	}
	rootBlock, err := blocks.NewBlockWithCid(rootNode.RawData(), rootNode.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if err := bs.Blockstore().Put(ctx, rootBlock); err != nil {
		t.Fatal(err)
	}

	// Resolver fails — should fall back
	resolver := &mockDAGResolver{
		respond: func(rootCid string) ([]DAGNode, error) {
			return nil, fmt.Errorf("portal down")
		},
	}

	p := newTestPrewarmerWithDAG(t, bs, resolver)
	defer p.Stop()

	visited, skipped, _, err := p.prewarm(ctx, rootNode.Cid())
	if err != nil {
		t.Fatalf("prewarm should succeed via fallback, got error: %v", err)
	}
	if visited == 0 {
		t.Fatal("expected some blocks visited via fallback walk")
	}
	_ = skipped
}

// TestPrewarm_ResolverNil_FallsBackToWalk verifies that when dagResolver
// is nil, prewarm uses the sequential walk path.
func TestPrewarm_ResolverNil_FallsBackToWalk(t *testing.T) {
	bs := newTestBlockService(t)

	childData := []byte("child")
	childBlock := makeRawBlock(t, childData)
	rootNode := makeDirNode(t, &format.Link{
		Cid:  childBlock.Cid(),
		Name: "child",
		Size: uint64(len(childData)),
	})

	ctx := context.Background()
	if err := bs.Blockstore().Put(ctx, childBlock); err != nil {
		t.Fatal(err)
	}
	rootBlock, err := blocks.NewBlockWithCid(rootNode.RawData(), rootNode.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if err := bs.Blockstore().Put(ctx, rootBlock); err != nil {
		t.Fatal(err)
	}

	// nil resolver — should use sequential walk
	p := newTestPrewarmerWithDAG(t, bs, nil)
	defer p.Stop()

	visited, _, _, err := p.prewarm(ctx, rootNode.Cid())
	if err != nil {
		t.Fatalf("prewarm failed: %v", err)
	}
	if visited == 0 {
		t.Fatal("expected blocks visited via sequential walk")
	}
}

// TestWalkBatch_SkipsInvalidCIDs verifies that invalid CID strings
// from the resolver are skipped, not fatal.
func TestWalkBatch_SkipsInvalidCIDs(t *testing.T) {
	bs := newTestBlockService(t)

	validBlock := makeRawBlock(t, []byte("valid"))
	if err := bs.Blockstore().Put(context.Background(), validBlock); err != nil {
		t.Fatal(err)
	}

	resolver := &mockDAGResolver{
		respond: func(rootCid string) ([]DAGNode, error) {
			return []DAGNode{
				{CID: "invalid-cid-string", Size: 1, Children: nil},
				{CID: validBlock.Cid().String(), Size: 5, Children: nil},
			}, nil
		},
	}

	p := newTestPrewarmerWithDAG(t, bs, resolver)
	defer p.Stop()

	visited, skipped, _, err := p.walkBatch(context.Background(), validBlock.Cid())
	if err != nil {
		t.Fatalf("walkBatch failed: %v", err)
	}
	if visited != 1 {
		t.Fatalf("expected 1 visited (valid block), got %d", visited)
	}
	if skipped != 1 {
		t.Fatalf("expected 1 skipped (invalid CID), got %d", skipped)
	}
}
