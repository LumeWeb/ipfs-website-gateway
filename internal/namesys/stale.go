package namesys

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gammazero/workerpool"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	ci "github.com/libp2p/go-libp2p/core/crypto"
	"go.uber.org/zap"
)

var (
	errNoResult = errors.New("no IPNS resolution result")
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
	if se, ok := s.store.GetStale(p.String()); ok {
		age := time.Since(se.cachedAt)
		if age < s.store.freshTTL {
			s.logger.Debug("serving fresh ipns entry from cache",
				zap.String("path", p.String()),
				zap.Duration("age", age),
			)
			return se.result, nil
		}
		s.logger.Debug("serving stale ipns entry, triggering background revalidation",
			zap.String("path", p.String()),
			zap.Duration("age", age),
		)
		s.revalidate(p, opts)
		return se.result, nil
	}

	type resolveOutcome struct {
		result namesys.Result
		err    error
	}

	resolveCh := make(chan resolveOutcome, 1)
	go func() {
		result, err := s.inner.Resolve(ctx, p, opts...)
		resolveCh <- resolveOutcome{result: result, err: err}
	}()

	timeout := s.store.timeout
	select {
	case outcome := <-resolveCh:
		if outcome.err == nil {
			s.store.PutStale(p.String(), staleEntry{result: outcome.result, cachedAt: time.Now()})
			s.logger.Debug("ipns resolve succeeded (fresh)",
				zap.String("path", p.String()),
				zap.Duration("ttl", outcome.result.TTL),
			)
			return outcome.result, nil
		}

		s.logger.Debug("ipns resolve failed, no stale fallback available",
			zap.String("path", p.String()),
			zap.Error(outcome.err),
		)
		return namesys.Result{}, outcome.err

	case <-time.After(timeout):
		s.logger.Debug("ipns resolve timed out, no stale fallback available",
			zap.String("path", p.String()),
			zap.Duration("timeout", timeout),
		)
		return namesys.Result{}, context.DeadlineExceeded
	}
}

func (s *StaleWhileRevalidateNameSystem) ResolveAsync(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) <-chan namesys.AsyncResult {
	if se, ok := s.store.GetStale(p.String()); ok {
		age := time.Since(se.cachedAt)
		if age < s.store.freshTTL {
			s.logger.Debug("serving fresh ipns async entry from cache",
				zap.String("path", p.String()),
				zap.Duration("age", age),
			)
			ch := make(chan namesys.AsyncResult, 1)
			ch <- namesys.AsyncResult{Path: se.result.Path, TTL: se.result.TTL, LastMod: se.result.LastMod}
			close(ch)
			return ch
		}
		s.logger.Debug("serving stale ipns async entry, triggering background revalidation",
			zap.String("path", p.String()),
			zap.Duration("age", age),
		)
		s.revalidate(p, opts)
		ch := make(chan namesys.AsyncResult, 1)
		ch <- namesys.AsyncResult{Path: se.result.Path, TTL: se.result.TTL, LastMod: se.result.LastMod}
		close(ch)
		return ch
	}

	resolveCtx, cancel := context.WithTimeout(ctx, s.store.timeout)

	innerCh := s.inner.ResolveAsync(resolveCtx, p, opts...)
	ch := make(chan namesys.AsyncResult, 1)

	var best namesys.AsyncResult
	var lastErr error
	hadResult := false

	go func() {
		defer close(ch)
		defer cancel()

		for res := range innerCh {
			if res.Err != nil {
				lastErr = res.Err
				continue
			}
			best = res
			hadResult = true
		}

		if hadResult {
			s.store.PutStale(p.String(), staleEntry{
				result: namesys.Result{
					Path:    best.Path,
					TTL:     best.TTL,
					LastMod: best.LastMod,
				},
				cachedAt: time.Now(),
			})
			s.logger.Debug("ipns async resolve succeeded (fresh)",
				zap.String("path", p.String()),
			)
			ch <- best
		} else {
			s.logger.Debug("ipns async resolve failed, no stale fallback available",
				zap.String("path", p.String()),
				zap.Error(lastErr),
			)
			if lastErr != nil {
				ch <- namesys.AsyncResult{Err: lastErr}
			} else {
				ch <- namesys.AsyncResult{Err: errNoResult}
			}
		}
	}()

	return ch
}

func (s *StaleWhileRevalidateNameSystem) Publish(ctx context.Context, sk ci.PrivKey, value path.Path, opts ...namesys.PublishOption) error {
	return s.inner.Publish(ctx, sk, value, opts...)
}

func (s *StaleWhileRevalidateNameSystem) Stop() {
	s.pool.StopWait()
	_ = s.store.Close()
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
