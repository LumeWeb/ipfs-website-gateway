package namesys

import (
	"context"
	"sync"
	"time"

	"github.com/gammazero/workerpool"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	ci "github.com/libp2p/go-libp2p/core/crypto"
	"go.uber.org/zap"
)

type staleEntry struct {
	result   namesys.Result
	cachedAt time.Time
}

type StaleWhileRevalidateNameSystem struct {
	inner   namesys.NameSystem
	store   *IPNSStore
	pending sync.Map
	pool    *workerpool.WorkerPool
	logger  *zap.Logger
}

func NewStaleWhileRevalidateNameSystem(inner namesys.NameSystem, store *IPNSStore, maxWorkers int, logger *zap.Logger) *StaleWhileRevalidateNameSystem {
	if maxWorkers <= 0 {
		maxWorkers = 4
	}
	return &StaleWhileRevalidateNameSystem{
		inner:  inner,
		store:  store,
		pool:   workerpool.New(maxWorkers),
		logger: logger,
	}
}

func (s *StaleWhileRevalidateNameSystem) Resolve(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
	result, err := s.inner.Resolve(ctx, p, opts...)
	if err == nil {
		s.store.PutStale(p.String(), staleEntry{result: result, cachedAt: time.Now()})
		return result, nil
	}

	if se, ok := s.store.GetStale(p.String()); ok {
		if time.Since(se.cachedAt) < s.store.staleTTL {
			s.logger.Debug("serving stale namesys entry while revalidating",
				zap.String("path", p.String()),
				zap.Duration("age", time.Since(se.cachedAt)),
			)
			s.revalidate(p, opts)
			return se.result, nil
		}
		s.store.DeleteStale(p.String())
	}

	return namesys.Result{}, err
}

func (s *StaleWhileRevalidateNameSystem) ResolveAsync(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) <-chan namesys.AsyncResult {
	result, err := s.inner.Resolve(ctx, p, opts...)
	ch := make(chan namesys.AsyncResult, 1)
	if err == nil {
		s.store.PutStale(p.String(), staleEntry{result: result, cachedAt: time.Now()})
		ch <- namesys.AsyncResult{Path: result.Path, TTL: result.TTL, LastMod: result.LastMod}
	} else if se, ok := s.store.GetStale(p.String()); ok {
		if time.Since(se.cachedAt) < s.store.staleTTL {
			s.revalidate(p, opts)
			ch <- namesys.AsyncResult{Path: se.result.Path, TTL: se.result.TTL, LastMod: se.result.LastMod}
		} else {
			s.store.DeleteStale(p.String())
			ch <- namesys.AsyncResult{Err: err}
		}
	} else {
		ch <- namesys.AsyncResult{Err: err}
	}
	close(ch)
	return ch
}

func (s *StaleWhileRevalidateNameSystem) Publish(ctx context.Context, sk ci.PrivKey, value path.Path, opts ...namesys.PublishOption) error {
	return s.inner.Publish(ctx, sk, value, opts...)
}

func (s *StaleWhileRevalidateNameSystem) Stop() {
	s.pool.StopWait()
	s.store.Close()
}

func (s *StaleWhileRevalidateNameSystem) revalidate(p path.Path, opts []namesys.ResolveOption) {
	key := p.String()
	if _, pending := s.pending.LoadOrStore(key, struct{}{}); pending {
		return
	}

	s.pool.Submit(func() {
		defer s.pending.Delete(key)

		ctx, cancel := context.WithTimeout(context.Background(), s.store.timeout)
		defer cancel()

		result, err := s.inner.Resolve(ctx, p, opts...)
		if err != nil {
			s.logger.Debug("background revalidation failed",
				zap.String("path", p.String()),
				zap.Error(err),
			)
			return
		}

		s.store.PutStale(key, staleEntry{result: result, cachedAt: time.Now()})
		s.logger.Debug("background revalidation succeeded",
			zap.String("path", p.String()),
		)
	})
}
