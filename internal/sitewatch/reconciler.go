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
	api     ReconcilerAPI
	cache   ReconcilerCache
	cursor  CursorStore
	prewarm ReconcileHandler
	logger  *zap.Logger

	mu        sync.Mutex
	connected bool
	ready     bool
}

// NewReconciler creates a reconciler. prewarm may be nil; published changes
// still invalidate the cache.
func NewReconciler(api ReconcilerAPI, cache ReconcilerCache, cursor CursorStore, prewarm ReconcileHandler, logger *zap.Logger) *Reconciler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Reconciler{
		api:     api,
		cache:   cache,
		cursor:  cursor,
		prewarm: prewarm,
		logger:  logger.Named("sse-reconciler"),
	}
}

// OnConnect feeds connection-state transitions from the SSE client. A
// disconnected→connected transition triggers a reconciliation run; repeat
// connected reports while already connected are no-ops.
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
	// Repeat connected reports (e.g. the telemetry poller) are no-ops; only the
	// disconnected→connected transition reconciles. One run per connection.
	if r.connected {
		r.mu.Unlock()
		return
	}
	r.connected = true
	r.mu.Unlock()

	go r.run()
}

// Ready reports whether a reconciliation has completed up to the journal
// high-water mark since the last reconnect. The broken-site watcher must not
// clear its recovery state until this is true.
func (r *Reconciler) Ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready
}

func (r *Reconciler) run() {
	ctx := context.Background()
	after, err := r.cursor.Get(ctx)
	if err != nil {
		r.logger.Warn("failed to load SSE cursor, starting from retained window",
			zap.Error(err))
	}

	replayed := 0
	for {
		resp, err := r.api.ReconcileWebsiteChanges(ctx, strconv.Itoa(after))
		if err != nil {
			r.logger.Error("SSE reconciliation failed", zap.Error(err))
			return
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

	r.mu.Lock()
	r.ready = true
	r.mu.Unlock()
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
