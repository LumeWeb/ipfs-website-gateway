package cache

import (
	"context"
	"testing"

	blocks "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	"go.uber.org/zap"
)

func TestContentBlockstore_PutAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c, err := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	if err != nil {
		t.Fatalf("cid.Decode: %v", err)
	}

	data := []byte("hello world")
	blk, err := blocks.NewBlockWithCid(data, c)
	if err != nil {
		t.Fatalf("NewBlockWithCid: %v", err)
	}

	if err := bs.Put(ctx, blk); err != nil {
		t.Fatalf("Put: %v", err)
	}

	retrieved, err := bs.Get(ctx, c)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if string(retrieved.RawData()) != string(data) {
		t.Errorf("expected %q, got %q", string(data), string(retrieved.RawData()))
	}
}

func TestContentBlockstore_Has(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")

	has, err := bs.Has(ctx, c)
	if err != nil {
		t.Fatalf("Has (empty): %v", err)
	}
	if has {
		t.Error("expected Has=false for non-existent block")
	}

	blk, _ := blocks.NewBlockWithCid([]byte("data"), c)
	_ = bs.Put(ctx, blk)

	has, err = bs.Has(ctx, c)
	if err != nil {
		t.Fatalf("Has (present): %v", err)
	}
	if !has {
		t.Error("expected Has=true for existing block")
	}
}

func TestContentBlockstore_DeleteBlock(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	blk, _ := blocks.NewBlockWithCid([]byte("data"), c)
	_ = bs.Put(ctx, blk)

	if err := bs.DeleteBlock(ctx, c); err != nil {
		t.Fatalf("DeleteBlock: %v", err)
	}

	has, _ := bs.Has(ctx, c)
	if has {
		t.Error("expected block to be deleted")
	}
}

func TestContentBlockstore_GetSize(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	data := []byte("hello world")
	blk, _ := blocks.NewBlockWithCid(data, c)
	_ = bs.Put(ctx, blk)

	size, err := bs.GetSize(ctx, c)
	if err != nil {
		t.Fatalf("GetSize: %v", err)
	}
	if size != len(data) {
		t.Errorf("expected size %d, got %d", len(data), size)
	}
}

func TestContentBlockstore_PutMany(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c1, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	c2, _ := cid.Decode("QmPZ9gcCEpqKTo6aq61g2nXGUhM4iCL3ewB6LDXZCtioio")

	blk1, _ := blocks.NewBlockWithCid([]byte("block1"), c1)
	blk2, _ := blocks.NewBlockWithCid([]byte("block2"), c2)

	if err := bs.PutMany(ctx, []blocks.Block{blk1, blk2}); err != nil {
		t.Fatalf("PutMany: %v", err)
	}

	has1, _ := bs.Has(ctx, c1)
	has2, _ := bs.Has(ctx, c2)
	if !has1 || !has2 {
		t.Errorf("expected both blocks to exist, got has1=%v has2=%v", has1, has2)
	}
}

func TestContentBlockstore_View(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	data := []byte("view test data")
	blk, _ := blocks.NewBlockWithCid(data, c)
	_ = bs.Put(ctx, blk)

	var viewed []byte
	err = bs.View(ctx, c, func(b []byte) error {
		viewed = make([]byte, len(b))
		copy(viewed, b)
		return nil
	})
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	if string(viewed) != string(data) {
		t.Errorf("expected %q, got %q", string(data), string(viewed))
	}
}

func TestContentBlockstore_AllKeysChan(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c1, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
	c2, _ := cid.Decode("QmPZ9gcCEpqKTo6aq61g2nXGUhM4iCL3ewB6LDXZCtioio")

	blk1, _ := blocks.NewBlockWithCid([]byte("block1"), c1)
	blk2, _ := blocks.NewBlockWithCid([]byte("block2"), c2)
	_ = bs.Put(ctx, blk1)
	_ = bs.Put(ctx, blk2)

	ch, err := bs.AllKeysChan(ctx)
	if err != nil {
		t.Fatalf("AllKeysChan: %v", err)
	}

	found := map[string]bool{}
	for c := range ch {
		found[c.String()] = true
	}

	if len(found) != 2 {
		t.Errorf("expected 2 keys, got %d", len(found))
	}
}

func TestContentBlockstore_Cache(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	if bs.Cache() != cc {
		t.Error("expected Cache() to return the underlying ContentCache")
	}
}

func TestContentBlockstore_GetReturnsErrNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cc, err := NewContentCache(tmpDir, 10*1024*1024, 100000)
	if err != nil {
		t.Fatalf("NewContentCache: %v", err)
	}

	bs := NewContentBlockstore(cc, zap.NewNop())
	ctx := context.Background()

	c, _ := cid.Decode("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")

	_, err = bs.Get(ctx, c)
	if !ipld.IsNotFound(err) {
		t.Errorf("expected ipld.ErrNotFound, got %v", err)
	}

	err = bs.View(ctx, c, func(b []byte) error { return nil })
	if !ipld.IsNotFound(err) {
		t.Errorf("expected ipld.ErrNotFound, got %v", err)
	}
}
