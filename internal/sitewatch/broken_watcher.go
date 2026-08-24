// Package sitewatch provides a recovery watcher for sites marked broken while
// the portal SSE stream is disconnected. When the SSE client drops, any site
// served as broken (410) is tracked and re-polled via the portal API until it
// recovers, so a missed site_published event during the reconnect gap is not
// permanently lost.
package sitewatch

import (
	"context"
	"errors"
	"sync"
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

// APIClient is the subset of the portal API the watcher needs to re-poll a
// site's status while SSE is disconnected.
type APIClient interface {
	GetWebsite(ctx context.Context, domain string) (*types.GatewayWebsiteResponse, error)
}

// StatusCache invalidates a cached status entry once a site recovers.
type StatusCache interface {
	Invalidate(domain string)
}

// ConnState reports whether the SSE stream is currently connected.
type ConnState interface {
	IsConnected() bool
}

// RecoverHandler is invoked when a watched broken site reports active again.
// It receives the site's target hash so callers can act on it (e.g. prewarm)
// without re-fetching from the API.
type RecoverHandler func(domain, targetHash string)

// BrokenWatcher tracks sites served as broken while SSE is disconnected and
// polls them via the portal API. A site is dropped from the watch set once it
// reports active again (recovery), as soon as SSE reconnects, or when a 404
// (deleted) status has been confirmed across deletedConfirmCount polls.
type BrokenWatcher struct {
	api       APIClient
	cache     StatusCache
	conn      ConnState
	interval  time.Duration
	onRecover RecoverHandler
	logger    *zap.Logger

	// deletedConfirmCount is the number of consecutive 404 responses required
	// before a site is treated as deleted and dropped from the watch set.
	// A single 404 can be transient (content not yet pinned/replicated), so it
	// is not treated as permanent deletion on its own.
	deletedConfirmCount int

	pollTimeout time.Duration

	mu     sync.Mutex
	broken map[string]struct{}
	// notFound tracks consecutive 404 responses per domain while disconnected.
	notFound map[string]int

	done      chan struct{}
	stopOnce  sync.Once
	loopWG    sync.WaitGroup
	recoverWG sync.WaitGroup
}

// NewBrokenWatcher creates a recovery watcher. The watcher is not started
// until Start is called. deletedConfirmCount is the number of consecutive 404
// responses needed before a site is considered deleted; values <= 0 default
// to 3.
func NewBrokenWatcher(apiClient APIClient, cache StatusCache, conn ConnState, interval time.Duration, deletedConfirmCount int, onRecover RecoverHandler, logger *zap.Logger) *BrokenWatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if deletedConfirmCount <= 0 {
		deletedConfirmCount = 3
	}

	return &BrokenWatcher{
		api:                 apiClient,
		cache:               cache,
		conn:                conn,
		interval:            interval,
		deletedConfirmCount: deletedConfirmCount,
		onRecover:           onRecover,
		logger:              logger.Named("broken-watcher"),
		pollTimeout:         10 * time.Second,
		broken:              make(map[string]struct{}),
		notFound:            make(map[string]int),
		done:                make(chan struct{}),
	}
}

// MarkBroken records a domain as broken so it will be polled while SSE is
// disconnected. It is idempotent and safe to call concurrently.
func (w *BrokenWatcher) MarkBroken(domain string) {
	if domain == "" {
		return
	}

	w.mu.Lock()
	w.broken[domain] = struct{}{}
	w.mu.Unlock()

	w.logger.Debug("tracking broken site for recovery polling", zap.String("domain", domain))
}

// Start launches the polling loop. It stops after Stop is called.
func (w *BrokenWatcher) Start() {
	w.loopWG.Add(1)
	go w.loop()
}

// Stop terminates the polling loop and waits for it and any in-flight recover
// callbacks to finish.
func (w *BrokenWatcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)
	})
	w.loopWG.Wait()    // loop finished, no further recover() calls can be made
	w.recoverWG.Wait() // drain in-flight recover callbacks
}

// loop ticks on the configured interval. While SSE is connected the watch set
// is cleared (no gap, events are delivered directly). While disconnected it
// polls each tracked broken domain for recovery.
func (w *BrokenWatcher) loop() {
	defer w.loopWG.Done()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			if w.conn.IsConnected() {
				w.clear()
				continue
			}
			w.poll()
		}
	}
}

// clear empties the watch set. Called once SSE reconnects.
func (w *BrokenWatcher) clear() {
	w.mu.Lock()
	n := len(w.broken)
	if n > 0 {
		w.broken = make(map[string]struct{})
	}
	w.notFound = make(map[string]int)
	w.mu.Unlock()

	if n > 0 {
		w.logger.Info("SSE reconnected, cleared broken site watch set",
			zap.Int("cleared", n))
	}
}

// poll re-fetches the status of every tracked broken domain. Any domain that
// now reports active is considered recovered: its cache entry is invalidated,
// the recover callback fires, and it is removed from the watch set.
func (w *BrokenWatcher) poll() {
	w.mu.Lock()
	domains := make([]string, 0, len(w.broken))
	for d := range w.broken {
		domains = append(domains, d)
	}
	w.mu.Unlock()

	if len(domains) == 0 {
		return
	}

	w.logger.Debug("SSE disconnected, polling broken sites",
		zap.Int("count", len(domains)))

	for _, domain := range domains {
		ctx, cancel := context.WithTimeout(context.Background(), w.pollTimeout)
		website, err := w.api.GetWebsite(ctx, domain)
		cancel()

		// A 404 indicates the site is not present, which may be a transient
		// condition (content not yet pinned/replicated) or a permanent delete.
		// Only once the 404 is confirmed across deletedConfirmCount consecutive
		// polls is the site dropped from the watch set.
		if err != nil {
			if errors.Is(err, ipfs.ErrNotFound) {
				if w.confirmNotFound(domain) {
					w.mu.Lock()
					delete(w.broken, domain)
					w.mu.Unlock()
					w.logger.Info("site deleted, dropped from broken watch set",
						zap.String("domain", domain))
				}
				continue
			}
			// Any non-404 outcome interrupts the confirmation streak.
			w.resetNotFound(domain)
			w.logger.Debug("broken site poll failed, will retry",
				zap.String("domain", domain),
				zap.Error(err))
			continue
		}

		// A successful response (active or still broken/gone) proves the site
		// exists, so any 404 confirmation streak is reset.
		w.resetNotFound(domain)

		if website != nil && types.Classify(website).IsActive() {
			w.recover(domain, website.TargetHash)
		}
	}
}

// confirmNotFound records a consecutive 404 for a domain and reports whether
// it has reached the configured deletion threshold.
func (w *BrokenWatcher) confirmNotFound(domain string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.notFound[domain]++
	return w.notFound[domain] >= w.deletedConfirmCount
}

// resetNotFound clears the consecutive-404 counter for a domain. Called on any
// non-404 poll outcome so a transient blip cannot accumulate toward deletion.
func (w *BrokenWatcher) resetNotFound(domain string) {
	w.mu.Lock()
	delete(w.notFound, domain)
	w.mu.Unlock()
}

// recover removes a domain from the watch set, invalidates its cached status,
// and fires the recover callback off the polling loop so a slow callback does
// not stall polling of the remaining tracked sites.
func (w *BrokenWatcher) recover(domain, targetHash string) {
	w.mu.Lock()
	delete(w.broken, domain)
	delete(w.notFound, domain)
	w.mu.Unlock()

	if w.cache != nil {
		w.cache.Invalidate(domain)
	}

	if w.onRecover != nil {
		w.recoverWG.Add(1)
		go func() {
			defer w.recoverWG.Done()
			w.onRecover(domain, targetHash)
		}()
	}

	w.logger.Info("broken site recovered, invalidated cache",
		zap.String("domain", domain))
}
