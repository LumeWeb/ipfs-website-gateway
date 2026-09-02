package sitewatch

import (
	"context"
	"strconv"
	"sync"

	sdk "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/internal/event"
	"go.uber.org/zap"
)

// ReconcilerAPI replays durable website lifecycle changes after a cursor.
type ReconcilerAPI interface {
	ReconcileWebsiteChanges(ctx context.Context, after string) (*sdk.WebsiteChangesResponse, error)
}

// ReconcilerCache invalidates a cached status entry when a change is replayed.
type ReconcilerCache interface {
	Invalidate(domain string)
}

// ReconcileHandler is invoked for each replayed published change so the gateway
// can prewarm the new content. It mirrors the realtime SSE published handler.
type ReconcileHandler func(domain, cid string)

// CursorStore persists the last fully-processed high-water mark.
type CursorStore interface {
	Get(ctx context.Context) (int, error)
	Set(ctx context.Context, mark int) error
}

// Reconciler catches up on website lifecycle changes missed during an SSE
// disconnect by replaying the portal's durable change journal
// (ReconcileWebsiteChanges) whenever the stream (re)connects. Publishing a
// change invalidates the domain's cached status and, for publishes, submits the
// new CID for prewarm — mirroring the realtime SSE handler so a gap cannot lose
// a deployment. It only reports ready (Ready) once a reconcile has completed up
// to the journal high-water mark, letting the BrokenWatcher defer clearing its
// recovery state until then.
type Reconciler struct {
	ctx     context.Context
	api     ReconcilerAPI
	cache   ReconcilerCache
	cursor  CursorStore
	prewarm ReconcileHandler

	mu        sync.Mutex
	connected bool
	ready     bool
	running   bool
	pending   bool
	// generation identifies the current connection session. A run captures the
	// generation it was launched for and may only mark Ready if it is still the
	// current session when it completes (a stale/overlapping run cannot clear
	// the broken-site recovery state before the fresh reconcile finishes).
	generation uint64
	logger     *zap.Logger
}

// NewReconciler creates a reconciler bound to ctx, the application lifecycle
// context. Reconciliation runs propagate ctx cancellation (e.g. shutdown) to
// the cursor store and portal API. prewarm may be nil; published changes still
// invalidate the cache.
func NewReconciler(ctx context.Context, api ReconcilerAPI, cache ReconcilerCache, cursor CursorStore, prewarm ReconcileHandler, logger *zap.Logger) *Reconciler {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Reconciler{
		ctx:     ctx,
		api:     api,
		cache:   cache,
		cursor:  cursor,
		prewarm: prewarm,
		logger:  logger.Named("sse-reconciler"),
	}
}

// OnConnect feeds connection-state transitions from the SSE client. A
// disconnected→connected transition triggers a reconciliation run; repeat
// connected reports while already connected are no-ops. Only one run executes at
// a time; a reconnect that arrives while a run is still draining is remembered
// and reconciled once the in-flight run completes.
func (r *Reconciler) OnConnect(connected bool) {
	r.mu.Lock()
	if !connected {
		r.connected = false
		// The connection session is over; the next reconcile must complete
		// before the broken-site watcher is allowed to clear again.
		r.ready = false
		r.mu.Unlock()
		return
	}
	if r.connected {
		// Repeat connected report (e.g. the telemetry poller): no new session.
		r.mu.Unlock()
		return
	}

	// disconnected → connected transition: new connection session.
	r.connected = true
	r.ready = false
	r.generation++

	if r.running {
		// A prior reconcile is still draining; run again once it finishes.
		r.pending = true
		r.mu.Unlock()
		return
	}
	r.running = true
	gen := r.generation
	r.mu.Unlock()

	go r.run(gen)
}

// Ready reports whether a reconciliation has completed up to the journal
// high-water mark since the last reconnect. The broken-site watcher must not
// clear its recovery state until this is true.
func (r *Reconciler) Ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready
}

// run performs a reconciliation for the connection session captured by gen.
// Only the run that is still the current session may mark Ready; a stale run
// (superseded by a newer disconnect/reconnect) must not clear broken-site
// recovery state early.
func (r *Reconciler) run(gen uint64) {
	err := r.reconcile()

	r.mu.Lock()
	// Only a run that is still the current (connected) session may report ready.
	if err == nil && r.connected && r.generation == gen {
		r.ready = true
	}

	if r.pending {
		// A newer connection started while this run drained; reconcile it now.
		r.pending = false
		gen = r.generation
		r.mu.Unlock()
		go r.run(gen)
		return
	}

	r.running = false
	r.mu.Unlock()
}

// reconcile replays the durable change journal after the persisted cursor,
// paging past truncation until the journal high-water mark is reached.
func (r *Reconciler) reconcile() error {
	ctx := r.ctx
	after, err := r.cursor.Get(ctx)
	if err != nil {
		r.logger.Warn("failed to load SSE cursor, starting from retained window",
			zap.Error(err))
	}

	replayed := 0
	for {
		if err := ctx.Err(); err != nil {
			r.logger.Debug("SSE reconciliation aborted", zap.Error(err))
			return err
		}

		resp, err := r.api.ReconcileWebsiteChanges(ctx, strconv.Itoa(after))
		if err != nil {
			r.logger.Error("SSE reconciliation failed", zap.Error(err))
			return err
		}

		for _, ev := range resp.Events {
			r.apply(ev)
			replayed++
		}

		after = resp.HighWaterMark
		if err := r.cursor.Set(ctx, after); err != nil {
			r.logger.Warn("failed to persist SSE cursor", zap.Error(err))
		}

		if !resp.Truncated {
			break
		}
	}

	if replayed > 0 {
		event.RecordReplayEvents(replayed)
		r.logger.Info("SSE reconciliation replayed missed changes",
			zap.Int("replayed", replayed),
			zap.Int("high_water_mark", after),
		)
	}

	return nil
}

// apply dispatches a single replayed change, mirroring the realtime SSE handler.
func (r *Reconciler) apply(ev sdk.WebsiteChangeEvent) {
	switch ev.EventType {
	case sdk.WebsiteChangeEventPublished:
		if ev.Domain != "" && r.cache != nil {
			r.cache.Invalidate(ev.Domain)
		}
		if ev.Cid != nil && *ev.Cid != "" && ev.Domain != "" && r.prewarm != nil {
			r.prewarm(ev.Domain, *ev.Cid)
		}
	case sdk.WebsiteChangeEventRemoved:
		if ev.Domain != "" && r.cache != nil {
			r.cache.Invalidate(ev.Domain)
		}
	}
}
