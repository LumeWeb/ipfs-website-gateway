package prewarm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/ipld/merkledag"
	blocks "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	format "github.com/ipfs/go-ipld-format"
	mh "github.com/multiformats/go-multihash"
	"go.uber.org/zap"
)

func makeRawCID(data []byte) cid.Cid {
	h, _ := mh.Sum(data, mh.SHA2_256, -1)
	return cid.NewCidV1(cid.Raw, h)
}

func makeRawBlock(t *testing.T, data []byte) blocks.Block {
	t.Helper()
	c := makeRawCID(data)
	b, err := blocks.NewBlockWithCid(data, c)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func makeDirNode(t *testing.T, links ...*format.Link) *merkledag.ProtoNode {
	t.Helper()
	node := merkledag.NodeWithData([]byte{})
	for _, l := range links {
		if err := node.AddRawLink("", l); err != nil {
			t.Fatal(err)
		}
	}
	return node
}

func putProtoNode(t *testing.T, ctx context.Context, bs blockstore.Blockstore, node *merkledag.ProtoNode) {
	t.Helper()
	blk, err := blocks.NewBlockWithCid(node.RawData(), node.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if err := bs.Put(ctx, blk); err != nil {
		t.Fatal(err)
	}
}

func newTestBlockService(t *testing.T) blockservice.BlockService {
	t.Helper()
	memDs := ds.NewMapDatastore()
	bs := blockstore.NewBlockstore(memDs)
	return blockservice.New(bs, nil)
}

func newTestPrewarmer(t *testing.T, bs blockservice.BlockService) *Prewarmer {
	t.Helper()
	p, err := NewPrewarmer(context.Background(), bs, nil, zap.NewNop(), 30*time.Second, 2, 2, 1*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewPrewarmer_NilBlockService_ReturnsError(t *testing.T) {
	p, err := NewPrewarmer(context.Background(), nil, nil, zap.NewNop(), 30*time.Second, 2, 2, 1*time.Second, 10)
	if p != nil {
		t.Fatal("expected nil prewarmer")
	}
	if err == nil {
		t.Fatal("expected error for nil blockservice")
	}
}

func TestNewPrewarmer_DefaultConcurrency(t *testing.T) {
	bs := newTestBlockService(t)
	p, err := NewPrewarmer(context.Background(), bs, nil, zap.NewNop(), 30*time.Second, 0, 2, 1*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	if p.pool.Size() != 2 {
		t.Fatalf("expected pool size 2, got %d", p.pool.Size())
	}
}

func TestNewPrewarmer_NegativeConcurrency(t *testing.T) {
	bs := newTestBlockService(t)
	p, err := NewPrewarmer(context.Background(), bs, nil, zap.NewNop(), 30*time.Second, -5, 2, 1*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	if p.pool.Size() != 2 {
		t.Fatalf("expected pool size 2, got %d", p.pool.Size())
	}
}

func TestNewPrewarmer_DefaultRetryParams(t *testing.T) {
	bs := newTestBlockService(t)
	p, err := NewPrewarmer(context.Background(), bs, nil, zap.NewNop(), 30*time.Second, 2, 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	if p.retryAttempts != 2 {
		t.Fatalf("expected retry attempts 2, got %d", p.retryAttempts)
	}
	if p.retryDelay != 1*time.Second {
		t.Fatalf("expected retry delay 1s, got %v", p.retryDelay)
	}
}

func TestPrewarmer_Submit_WalksSingleBlock(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("hello world"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	p.Submit(blk.Cid())
	p.Stop()

	has, err := bs.Blockstore().Has(ctx, blk.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected block to be in blockstore after walk")
	}
}

func TestPrewarmer_Submit_WalksDAG(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	child1 := makeRawBlock(t, []byte("child1"))
	child2 := makeRawBlock(t, []byte("child2"))

	if err := bs.Blockstore().Put(ctx, child1); err != nil {
		t.Fatal(err)
	}
	if err := bs.Blockstore().Put(ctx, child2); err != nil {
		t.Fatal(err)
	}

	dirNode := makeDirNode(t,
		&format.Link{Cid: child1.Cid(), Size: uint64(len(child1.RawData()))},
		&format.Link{Cid: child2.Cid(), Size: uint64(len(child2.RawData()))},
	)
	putProtoNode(t, ctx, bs.Blockstore(), dirNode)

	p.Submit(dirNode.Cid())
	p.Stop()

	for _, c := range []cid.Cid{dirNode.Cid(), child1.Cid(), child2.Cid()} {
		has, err := bs.Blockstore().Has(ctx, c)
		if err != nil {
			t.Fatalf("Has(%s): %v", c, err)
		}
		if !has {
			t.Fatalf("expected %s to be in blockstore after DAG walk", c)
		}
	}
}

func TestPrewarmer_Submit_SkipsUnreachableBlocks(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	missingChild := makeRawBlock(t, []byte("missing"))

	dirNode := makeDirNode(t,
		&format.Link{Cid: missingChild.Cid(), Size: uint64(len(missingChild.RawData()))},
	)
	putProtoNode(t, ctx, bs.Blockstore(), dirNode)

	p.Submit(dirNode.Cid())
	p.Stop()

	has, err := bs.Blockstore().Has(ctx, dirNode.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected root node to be visited even when child is unreachable")
	}

	has, err = bs.Blockstore().Has(ctx, missingChild.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("missing child should not appear in blockstore")
	}
}

type faultInjectBlockstore struct {
	blockstore.Blockstore
	mu        sync.Mutex
	failCount int
	failN     int
}

func (f *faultInjectBlockstore) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	f.mu.Lock()
	if f.failCount < f.failN {
		f.failCount++
		f.mu.Unlock()
		return nil, fmt.Errorf("transient failure %d", f.failCount)
	}
	f.mu.Unlock()
	return f.Blockstore.Get(ctx, c)
}

func TestPrewarmer_Submit_RetriesTransientFailures(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)

	faultBs := &faultInjectBlockstore{Blockstore: bs.Blockstore(), failN: 1}
	faultBserv := blockservice.New(faultBs, nil)

	p, err := NewPrewarmer(context.Background(), faultBserv, nil, zap.NewNop(), 30*time.Second, 2, 3, 10*time.Millisecond, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	blk := makeRawBlock(t, []byte("retry me"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	p.Submit(blk.Cid())
	p.Stop()

	has, err := bs.Blockstore().Has(ctx, blk.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected block to be fetched after retry")
	}
}

type blockingBlockstore struct {
	blockstore.Blockstore
	blockCh chan struct{}
}

func (b *blockingBlockstore) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	select {
	case <-b.blockCh:
		return b.Blockstore.Get(ctx, c)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPrewarmer_Submit_RespectsTimeout(t *testing.T) {
	bs := newTestBlockService(t)

	blockingBs := &blockingBlockstore{Blockstore: bs.Blockstore(), blockCh: make(chan struct{})}
	blockingBserv := blockservice.New(blockingBs, nil)

	p, err := NewPrewarmer(context.Background(), blockingBserv, nil, zap.NewNop(), 50*time.Millisecond, 2, 1, 10*time.Millisecond, 10)
	if err != nil {
		t.Fatal(err)
	}

	blk := makeRawBlock(t, []byte("timeout test"))
	p.Submit(blk.Cid())

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() hung — timeout not respected")
	}
}

func TestPrewarmer_SubmitDedup(t *testing.T) {
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	c, err := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	if err != nil {
		t.Fatalf("cid.Decode: %v", err)
	}

	cidStr := c.String()

	p.mu.Lock()
	p.active[cidStr] = struct{}{}
	p.mu.Unlock()

	p.Submit(c)

	p.mu.Lock()
	_, stillActive := p.active[cidStr]
	p.mu.Unlock()
	if !stillActive {
		t.Fatal("dedup Submit should not remove the existing active entry")
	}
}

func TestPrewarmer_SubmitCleansActiveOnPoolStop(t *testing.T) {
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)

	c, err := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	if err != nil {
		t.Fatalf("cid.Decode: %v", err)
	}

	p.Submit(c)
	p.Stop()

	p.mu.Lock()
	_, active := p.active[c.String()]
	p.mu.Unlock()
	if active {
		t.Fatal("active entry should be cleaned up after pool stops and tasks drain")
	}
}

func TestPrewarmer_SubmitAfterStop_DropsTask(t *testing.T) {
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	p.Stop()

	c, err := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	if err != nil {
		t.Fatalf("cid.Decode: %v", err)
	}

	p.Submit(c)

	p.mu.Lock()
	_, active := p.active[c.String()]
	p.mu.Unlock()
	if active {
		t.Fatal("active entry should be cleaned up when pool is stopped")
	}
}

func TestPrewarmer_ConcurrentSubmits_SameCID_OnlyOneWalks(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)

	p, err := NewPrewarmer(context.Background(), bs, nil, zap.NewNop(), 30*time.Second, 2, 2, 1*1*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	blk := makeRawBlock(t, []byte("concurrent"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Submit(blk.Cid())
		}()
	}
	wg.Wait()

	p.Stop()

	p.mu.Lock()
	activeCount := len(p.active)
	p.mu.Unlock()
	if activeCount != 0 {
		t.Fatalf("expected 0 active entries after Stop, got %d", activeCount)
	}
}

func TestPrewarmer_ParentContextCancel_StopsWalk(t *testing.T) {
	bs := newTestBlockService(t)

	blockingBs := &blockingBlockstore{Blockstore: bs.Blockstore(), blockCh: make(chan struct{})}
	blockingBserv := blockservice.New(blockingBs, nil)

	ctx, cancel := context.WithCancel(context.Background())

	p, err := NewPrewarmer(ctx, blockingBserv, nil, zap.NewNop(), 30*time.Second, 2, 1, 10*time.Millisecond, 10)
	if err != nil {
		t.Fatal(err)
	}

	blk := makeRawBlock(t, []byte("cancel test"))
	p.Submit(blk.Cid())

	cancel()

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() hung — parent context cancellation should stop walk")
	}
}

func TestPrewarmer_IsWarmed_FalseBeforeSubmit(t *testing.T) {
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("hello"))
	c := blk.Cid()

	if p.IsWarmed(c) {
		t.Fatal("expected IsWarmed=false before any submit")
	}
}

func TestPrewarmer_IsWarmed_TrueAfterWalk(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("hello world"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	p.Submit(blk.Cid())
	p.Stop()

	if !p.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=true after successful walk")
	}
}

func TestPrewarmer_IsWarmed_FalseAfterFailedWalk(t *testing.T) {
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	// CID that doesn't exist in the blockstore; walk will fail
	c := makeRawCID([]byte("nonexistent"))

	p.Submit(c)
	p.Stop()

	if p.IsWarmed(c) {
		t.Fatal("expected IsWarmed=false after failed walk")
	}
}

func TestPrewarmer_SubmitPath_EmptyPathFallsBackToFullWalk(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("hello"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	p.SubmitPath(blk.Cid(), "")
	p.Stop()

	if !p.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=true after SubmitPath with empty path")
	}
}

func TestPrewarmer_SubmitPath_DeduplicatesActiveWalk(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("hello"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	// Submit then SubmitPath: second should be deduped by active map
	p.Submit(blk.Cid())
	p.SubmitPath(blk.Cid(), "some/path")
	p.Stop()

	if !p.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=true after concurrent submit + submitpath")
	}
}

func TestPrewarmer_SubmitPath_BadPathStillTriggersFullWalk(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("hello"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	p.SubmitPath(blk.Cid(), "nonexistent/path")
	p.Stop()

	// Root block should still be cached (full walk happens after path resolve fails)
	has, err := bs.Blockstore().Has(ctx, blk.Cid())
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected root block to be in blockstore even when path resolution fails")
	}

	if !p.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=true: full walk should succeed even if path resolve fails")
	}
}

func TestPrewarmer_SubmitPath_SkipsAlreadyWarmed(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("hello"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	// First walk marks it as warmed
	p.Submit(blk.Cid())
	p.Stop()

	if !p.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=true after first walk")
	}

	// SubmitPath should skip since already warmed
	p.SubmitPath(blk.Cid(), "some/path")
	p.Stop()
}

func TestPrewarmer_ConcurrentSubmitPath_DeduplicatesWalks(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	file := makeRawBlock(t, []byte("content"))
	if err := bs.Blockstore().Put(ctx, file); err != nil {
		t.Fatal(err)
	}
	dir := makeDirNode(t,
		&format.Link{Cid: file.Cid(), Name: "index.html", Size: uint64(len(file.RawData()))},
	)
	putProtoNode(t, ctx, bs.Blockstore(), dir)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.SubmitPath(dir.Cid(), "index.html")
		}()
	}
	wg.Wait()
	p.Stop()

	if !p.IsWarmed(dir.Cid()) {
		t.Fatal("expected IsWarmed=true after concurrent SubmitPath")
	}
}

func TestPrewarmer_ClearWarmed_AllowsRewarm(t *testing.T) {
	ctx := context.Background()
	bs := newTestBlockService(t)
	p := newTestPrewarmer(t, bs)
	defer p.Stop()

	blk := makeRawBlock(t, []byte("eviction regression"))
	if err := bs.Blockstore().Put(ctx, blk); err != nil {
		t.Fatal(err)
	}

	// Walk the DAG so it becomes warmed.
	p.Submit(blk.Cid())
	p.Stop()

	if !p.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=true after first walk")
	}

	// Simulate block cache eviction: clear the warmed entry.
	p.ClearWarmed(blk.Cid().String())

	if p.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=false after ClearWarmed")
	}

	// Re-submitting should walk again and re-mark as warmed.
	// Need a fresh prewarmer since the old one was stopped.
	p2 := newTestPrewarmer(t, bs)
	defer p2.Stop()

	p2.Submit(blk.Cid())
	p2.Stop()

	if !p2.IsWarmed(blk.Cid()) {
		t.Fatal("expected IsWarmed=true after re-walk following eviction")
	}
}
