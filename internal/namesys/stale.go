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

func resolvableKey(p path.Path) string {
	segments := p.Segments()
	if len(segments) >= 2 {
		rp, err := path.NewPathFromSegments(segments[0], segments[1])
		if err == nil {
			return rp.String()
		}
	}
	return p.String()
}

func stripSubPath(result namesys.Result) namesys.Result {
	segments := result.Path.Segments()
	if len(segments) <= 2 {
		return result
	}
	base, err := path.NewPathFromSegments(segments[0], segments[1])
	if err != nil {
		return result
	}
	return namesys.Result{Path: base, TTL: result.TTL, LastMod: result.LastMod}
}

func withSubPath(result namesys.Result, original path.Path) namesys.Result {
	segments := original.Segments()
	if len(segments) <= 2 {
		return result
	}
	joined, err := path.Join(result.Path, segments[2:]...)
	if err != nil {
		return result
	}
	return namesys.Result{Path: joined, TTL: result.TTL, LastMod: result.LastMod}
}

type staleEntry struct {
	result   namesys.Result
	cachedAt time.Time
}

type StaleWhileRevalidateNameSystem struct {
	inner        namesys.NameSystem
	store        *IPNSStore
	pending      sync.Map
	pool         *workerpool.WorkerPool
	logger       *zap.Logger
	watchers     sync.Map
	watchMu      sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	watchEnabled bool
}

func NewStaleWhileRevalidateNameSystem(inner namesys.NameSystem, store *IPNSStore, maxWorkers int, logger *zap.Logger) *StaleWhileRevalidateNameSystem {
	if maxWorkers <= 0 {
		maxWorkers = 4
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &StaleWhileRevalidateNameSystem{
		inner:  inner,
		store:  store,
		pool:   workerpool.New(maxWorkers),
		logger: logger.Named("namesys"),
		ctx:    ctx,
		cancel: cancel,
	}
	store.SetOnEvict(func(key string) {
		if v, ok := s.watchers.LoadAndDelete(key); ok {
			v.(*watcherState).cancel()
		}
	})
	return s
}

func (s *StaleWhileRevalidateNameSystem) EnableWatch() {
	s.watchEnabled = true
}

func (s *StaleWhileRevalidateNameSystem) WarmSubscriptions() {
	if !s.watchEnabled {
		return
	}

	keys := s.store.Keys()
	if len(keys) == 0 {
		return
	}

	s.logger.Info("warming pubsub subscriptions from cached IPNS entries",
		zap.Int("count", len(keys)),
	)

	for _, key := range keys {
		s.startWatcher(key)
	}
}

func (s *StaleWhileRevalidateNameSystem) Resolve(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
	key := resolvableKey(p)

	if se, ok := s.store.GetStale(key); ok {
		age := time.Since(se.cachedAt)
		if age < s.store.freshTTL {
			s.logger.Debug("cache hit, serving fresh entry",
				zap.String("key", key),
				zap.Duration("age", age),
			)
			return withSubPath(se.result, p), nil
		}
		s.logger.Debug("cache hit, serving stale entry, revalidating in background",
			zap.String("key", key),
			zap.Duration("age", age),
		)
		s.revalidate(p, opts)
		return withSubPath(se.result, p), nil
	}

	s.logger.Debug("cache miss, delegating to inner Resolve",
		zap.String("key", key),
		zap.Duration("timeout", s.store.timeout),
		zap.Bool("ctx_done", ctx.Err() != nil),
	)

	type resolveOutcome struct {
		result namesys.Result
		err    error
	}

	start := time.Now()
	resolveCh := make(chan resolveOutcome, 1)
	go func() {
		result, err := s.inner.Resolve(ctx, p, opts...)
		resolveCh <- resolveOutcome{result: result, err: err}
	}()

	timeout := s.store.timeout
	select {
	case outcome := <-resolveCh:
		elapsed := time.Since(start)
		if outcome.err == nil {
			s.store.PutStale(key, staleEntry{result: stripSubPath(outcome.result), cachedAt: time.Now()})
			s.startWatcher(key)
			s.logger.Debug("resolve succeeded",
				zap.String("key", key),
				zap.Duration("elapsed", elapsed),
				zap.Duration("ttl", outcome.result.TTL),
				zap.String("resolved_path", outcome.result.Path.String()),
			)
			return outcome.result, nil
		}

		s.logger.Debug("resolve failed, no stale fallback",
			zap.String("key", key),
			zap.Duration("elapsed", elapsed),
			zap.Error(outcome.err),
		)
		return namesys.Result{}, outcome.err

	case <-time.After(timeout):
		s.logger.Debug("resolve timed out, no stale fallback",
			zap.String("key", key),
			zap.Duration("timeout", timeout),
		)
		return namesys.Result{}, context.DeadlineExceeded
	}
}

func (s *StaleWhileRevalidateNameSystem) ResolveAsync(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) <-chan namesys.AsyncResult {
	key := resolvableKey(p)

	if se, ok := s.store.GetStale(key); ok {
		age := time.Since(se.cachedAt)
		if age < s.store.freshTTL {
			s.logger.Debug("cache hit, serving fresh async entry",
				zap.String("key", key),
				zap.Duration("age", age),
			)
			resolved := withSubPath(se.result, p)
			ch := make(chan namesys.AsyncResult, 1)
			ch <- namesys.AsyncResult{Path: resolved.Path, TTL: resolved.TTL, LastMod: resolved.LastMod}
			close(ch)
			return ch
		}
		s.logger.Debug("cache hit, serving stale async entry, revalidating in background",
			zap.String("key", key),
			zap.Duration("age", age),
		)
		s.revalidate(p, opts)
		resolved := withSubPath(se.result, p)
		ch := make(chan namesys.AsyncResult, 1)
		ch <- namesys.AsyncResult{Path: resolved.Path, TTL: resolved.TTL, LastMod: resolved.LastMod}
		close(ch)
		return ch
	}

	s.logger.Debug("cache miss, delegating to inner ResolveAsync",
		zap.String("key", key),
		zap.Duration("timeout", s.store.timeout),
	)

	resolveCtx, cancel := context.WithTimeout(ctx, s.store.timeout)

	start := time.Now()
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
				s.logger.Debug("async result error from inner",
					zap.String("key", key),
					zap.Error(res.Err),
				)
				continue
			}
			best = res
			hadResult = true
		}

		elapsed := time.Since(start)
		if hadResult {
			s.store.PutStale(key, staleEntry{
				result: namesys.Result{
					Path:    stripSubPath(namesys.Result{Path: best.Path, TTL: best.TTL, LastMod: best.LastMod}).Path,
					TTL:     best.TTL,
					LastMod: best.LastMod,
				},
				cachedAt: time.Now(),
			})
			s.startWatcher(key)
			s.logger.Debug("async resolve succeeded",
				zap.String("key", key),
				zap.Duration("elapsed", elapsed),
				zap.String("resolved_path", best.Path.String()),
			)
			ch <- best
		} else {
			s.logger.Debug("async resolve failed, no stale fallback",
				zap.String("key", key),
				zap.Duration("elapsed", elapsed),
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
	s.cancel()
	s.pool.StopWait()
	_ = s.store.Close()
}

func (s *StaleWhileRevalidateNameSystem) revalidate(p path.Path, opts []namesys.ResolveOption) {
	key := resolvableKey(p)
	if _, pending := s.pending.LoadOrStore(key, struct{}{}); pending {
		return
	}

	s.pool.Submit(func() {
		defer s.pending.Delete(key)

		ctx, cancel := context.WithTimeout(context.Background(), s.store.timeout)
		defer cancel()

		start := time.Now()
		result, err := s.inner.Resolve(ctx, p, opts...)
		elapsed := time.Since(start)
		if err != nil {
			s.logger.Debug("revalidation failed",
				zap.String("key", key),
				zap.Duration("elapsed", elapsed),
				zap.Error(err),
			)
			return
		}

		s.store.PutStale(key, staleEntry{result: stripSubPath(result), cachedAt: time.Now()})
		s.startWatcher(key)
		s.logger.Debug("revalidation succeeded",
			zap.String("key", key),
			zap.Duration("elapsed", elapsed),
			zap.String("resolved_path", result.Path.String()),
		)
	})
}

type watcherState struct {
	cancel context.CancelFunc
}

func (s *StaleWhileRevalidateNameSystem) startWatcher(key string) {
	if !s.watchEnabled {
		return
	}
	if _, exists := s.watchers.Load(key); exists {
		return
	}

	ctx, cancel := context.WithCancel(s.ctx)
	ws := &watcherState{cancel: cancel}

	if _, loaded := s.watchers.LoadOrStore(key, ws); loaded {
		cancel()
		return
	}

	p, err := path.NewPath(key)
	if err != nil {
		s.watchers.Delete(key)
		cancel()
		return
	}

	go s.watchLoop(ctx, key, ws, p)
}

func (s *StaleWhileRevalidateNameSystem) watchLoop(ctx context.Context, key string, ws *watcherState, p path.Path) {
	defer func() {
		if v, ok := s.watchers.Load(key); ok && v.(*watcherState) == ws {
			s.watchers.Delete(key)
		}
	}()

	ch := s.inner.ResolveAsync(ctx, p)

	for {
		select {
		case <-ctx.Done():
			return
		case res, ok := <-ch:
			if !ok {
				return
			}
			if res.Err != nil {
				continue
			}
			newPath := stripSubPath(namesys.Result{Path: res.Path, TTL: res.TTL, LastMod: res.LastMod}).Path
			if existing, ok := s.store.GetStale(key); ok && existing.result.Path.String() == newPath.String() {
				ch = s.inner.ResolveAsync(ctx, p)
				continue
			}
			s.store.PutStale(key, staleEntry{
				result: namesys.Result{
					Path:    newPath,
					TTL:     res.TTL,
					LastMod: res.LastMod,
				},
				cachedAt: time.Now(),
			})
			s.logger.Debug("pubsub cache update",
				zap.String("path", key),
				zap.String("new_path", newPath.String()),
			)
			ch = s.inner.ResolveAsync(ctx, p)
		}
	}
}
