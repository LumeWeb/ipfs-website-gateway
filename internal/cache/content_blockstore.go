package cache

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	blocks "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	"go.uber.org/zap"
)

type ContentBlockstore struct {
	cache  *ContentCache
	mu     sync.RWMutex
	logger *zap.Logger
}

func NewContentBlockstore(cache *ContentCache, logger *zap.Logger) *ContentBlockstore {
	return &ContentBlockstore{cache: cache, logger: logger}
}

func (cb *ContentBlockstore) DeleteBlock(ctx context.Context, c cid.Cid) error {
	cidStr := c.String()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	removed := false

	hashedPath := cb.cache.getBlockPath(cidStr)
	if _, err := os.Stat(hashedPath); err == nil {
		info, err := os.Stat(hashedPath)
		if err != nil {
			return fmt.Errorf("failed to stat block: %w", err)
		}
		if err := os.Remove(hashedPath); err != nil {
			return fmt.Errorf("failed to remove block: %w", err)
		}
		cb.cache.currentBytes -= info.Size()
		cb.cache.lru.Remove(cidStr)
		removed = true
	}

	if !removed {
		flatPath := filepath.Join(cb.cache.cachePath, cidStr)
		if _, err := os.Stat(flatPath); err == nil {
			info, err := os.Stat(flatPath)
			if err != nil {
				return fmt.Errorf("failed to stat flat block: %w", err)
			}
			if err := os.Remove(flatPath); err != nil {
				return fmt.Errorf("failed to remove flat block: %w", err)
			}
			cb.cache.currentBytes -= info.Size()
			cb.cache.lru.Remove(cidStr)
		}
	}

	cb.logger.Debug("blockstore delete", zap.String("cid", cidStr), zap.Bool("removed", removed))

	return nil
}

func (cb *ContentBlockstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	cidStr := c.String()

	cb.cache.mu.RLock()
	if cb.cache.lru.Contains(cidStr) {
		cb.cache.mu.RUnlock()
		return true, nil
	}
	cb.cache.mu.RUnlock()

	hashedPath := cb.cache.getBlockPath(cidStr)
	if _, err := os.Stat(hashedPath); err == nil {
		return true, nil
	}

	flatPath := filepath.Join(cb.cache.cachePath, cidStr)
	if _, err := os.Stat(flatPath); err == nil {
		return true, nil
	}

	return false, nil
}

func (cb *ContentBlockstore) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	data, err := cb.cache.Get(c.String())
	if err != nil {
		cb.logger.Debug("blockstore miss", zap.String("cid", c.String()))
		return nil, ipld.ErrNotFound{Cid: c}
	}

	cb.logger.Debug("blockstore hit", zap.String("cid", c.String()), zap.Int("size", len(data)))

	return blocks.NewBlockWithCid(data, c)
}

func (cb *ContentBlockstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	cidStr := c.String()

	hashedPath := cb.cache.getBlockPath(cidStr)
	if info, err := os.Stat(hashedPath); err == nil {
		return int(info.Size()), nil
	}

	flatPath := filepath.Join(cb.cache.cachePath, cidStr)
	if info, err := os.Stat(flatPath); err == nil {
		return int(info.Size()), nil
	}

	return -1, fmt.Errorf("block not found: %s", cidStr)
}

func (cb *ContentBlockstore) Put(ctx context.Context, blk blocks.Block) error {
	cb.logger.Debug("blockstore put", zap.String("cid", blk.Cid().String()), zap.Int("size", len(blk.RawData())))
	return cb.cache.Put(blk.Cid().String(), blk.RawData())
}

func (cb *ContentBlockstore) PutMany(ctx context.Context, blks []blocks.Block) error {
	for _, blk := range blks {
		if err := cb.Put(ctx, blk); err != nil {
			return err
		}
	}
	return nil
}

func (cb *ContentBlockstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	ch := make(chan cid.Cid)

	go func() {
		defer close(ch)

		cb.cache.mu.RLock()
		cachePath := cb.cache.cachePath
		cb.cache.mu.RUnlock()

		err := filepath.Walk(cachePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			cidStr := filepath.Base(path)
			c, err := cid.Decode(cidStr)
			if err != nil {
				return nil
			}

			select {
			case ch <- c:
			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		})

		if err != nil && err != context.Canceled {
			return
		}
	}()

	return ch, nil
}

func (cb *ContentBlockstore) View(ctx context.Context, c cid.Cid, callback func([]byte) error) error {
	data, err := cb.cache.Get(c.String())
	if err != nil {
		cb.logger.Debug("blockstore view miss", zap.String("cid", c.String()))
		return ipld.ErrNotFound{Cid: c}
	}
	return callback(data)
}

func (cb *ContentBlockstore) Close() error {
	return nil
}

func (cb *ContentBlockstore) Cache() *ContentCache {
	return cb.cache
}

var _ io.Closer = (*ContentBlockstore)(nil)
