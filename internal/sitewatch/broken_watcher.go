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
// reports active again (recovery) or as soon as SSE reconnects.
type BrokenWatcher struct {
	api       APIClient
	cache     StatusCache
	conn      ConnState
	interval  time.Duration
	onRecover RecoverHandler
	logger    *zap.Logger

	pollTimeout time.Duration

	mu     sync.Mutex
	broken map[string]struct{}

	done      chan struct{}
	stopOnce  sync.Once
	loopWG    sync.WaitGroup
	recoverWG sync.WaitGroup
}

// NewBrokenWatcher creates a recovery watcher. The watcher is not started
// until Start is called.
func NewBrokenWatcher(apiClient APIClient, cache StatusCache, conn ConnState, interval time.Duration, onRecover RecoverHandler, logger *zap.Logger) *BrokenWatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}

	return &BrokenWatcher{
		api:         apiClient,
		cache:       cache,
		conn:        conn,
		interval:    interval,
		onRecover:   onRecover,
		logger:      logger.Named("broken-watcher"),
		pollTimeout: 10 * time.Second,
		broken:      make(map[string]struct{}),
		done:        make(chan struct{}),
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

		// A deleted site (404) is permanently gone, so it is dropped from the
		// watch set instead of being polled forever. Broken/gone (410) and
		// transient errors stay in the set to be retried on the next interval.
		if err != nil {
			if errors.Is(err, ipfs.ErrNotFound) {
				w.mu.Lock()
				delete(w.broken, domain)
				w.mu.Unlock()
				w.logger.Info("site deleted, dropped from broken watch set",
					zap.String("domain", domain))
				continue
			}
			w.logger.Debug("broken site poll failed, will retry",
				zap.String("domain", domain),
				zap.Error(err))
			continue
		}

		if website != nil && types.Classify(website).IsActive() {
			w.recover(domain, website.TargetHash)
		}
	}
}

// recover removes a domain from the watch set, invalidates its cached status,
// and fires the recover callback off the polling loop so a slow callback does
// not stall polling of the remaining tracked sites.
func (w *BrokenWatcher) recover(domain, targetHash string) {
	w.mu.Lock()
	delete(w.broken, domain)
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
