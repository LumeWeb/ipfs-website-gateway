package prewarm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/gammazero/workerpool"
	"github.com/ipfs/boxo/blockservice"
	bsfetcher "github.com/ipfs/boxo/fetcher/impl/blockservice"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/path"
	"github.com/ipfs/boxo/path/resolver"
	cid "github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	_ "github.com/ipld/go-ipld-prime/codec/cbor"
	_ "github.com/ipld/go-ipld-prime/codec/dagcbor"
	_ "github.com/ipld/go-ipld-prime/codec/dagjson"
	_ "github.com/ipld/go-ipld-prime/codec/json"
	"github.com/ipfs/go-unixfsnode"
	dagpb "github.com/ipld/go-codec-dagpb"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type Prewarmer struct {
	bs            blockservice.BlockService
	resolver      resolver.Resolver
	logger        *zap.Logger
	timeout       time.Duration
	retryAttempts uint
	retryDelay    time.Duration
	ctx           context.Context

	mu     sync.Mutex
	active map[string]struct{}
	warmed sync.Map // cid.String() -> struct{}
	pool   *workerpool.WorkerPool
}

func NewPrewarmer(ctx context.Context, bs blockservice.BlockService, logger *zap.Logger, timeout time.Duration, maxConcurrency int, retryAttempts uint, retryDelay time.Duration) (*Prewarmer, error) {
	if bs == nil {
		return nil, fmt.Errorf("blockservice is required")
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 2
	}
	if retryAttempts == 0 {
		retryAttempts = 2
	}
	if retryDelay <= 0 {
		retryDelay = 1 * time.Second
	}
	fetcherCfg := bsfetcher.NewFetcherConfig(bs)
	fetcherCfg.PrototypeChooser = dagpb.AddSupportToChooser(bsfetcher.DefaultPrototypeChooser)
	fetcherFactory := fetcherCfg.WithReifier(unixfsnode.Reify)
	pathResolver := resolver.NewBasicResolver(fetcherFactory)

	return &Prewarmer{
		bs:            bs,
		resolver:      pathResolver,
		logger:        logger.Named("prewarm"),
		timeout:       timeout,
		retryAttempts: retryAttempts,
		retryDelay:    retryDelay,
		ctx:           ctx,
		active:        make(map[string]struct{}),
		pool:          workerpool.New(maxConcurrency),
	}, nil
}

// Submit enqueues a DAG walk for rootCID. If the CID is already being
// prewarmed the submission is skipped. If the pool is stopped the task
// is dropped — pre-warming is best-effort and will catch up on the
// next visitor hit.
func (p *Prewarmer) Submit(rootCID cid.Cid) {
	_, span := otel.TraceMethod(p.ctx, "Prewarmer.Submit",
		otel.WithAttributes(attribute.String("root_cid", rootCID.String())),
	)
	defer span.End()

	cidStr := rootCID.String()

	p.mu.Lock()
	if _, ok := p.active[cidStr]; ok {
		p.mu.Unlock()
		p.logger.Debug("prewarm already in progress, skipping", zap.String("cid", cidStr))
		return
	}
	p.active[cidStr] = struct{}{}
	p.mu.Unlock()

	prewarmSubmittedTotal.Inc()
	prewarmActiveWalks.Inc()

	err := p.pool.Do(func() {
		defer func() {
			p.mu.Lock()
			delete(p.active, cidStr)
			p.mu.Unlock()
			prewarmActiveWalks.Dec()
		}()

		ctx, cancel := context.WithTimeout(p.ctx, p.timeout)
		defer cancel()

		visited, skipped, elapsed, err := p.walk(ctx, rootCID)

		if err != nil {
			prewarmFailedTotal.Inc()
			p.logger.Warn("prewarm walk failed",
				zap.String("cid", cidStr),
				zap.Int("blocks_visited", visited),
				zap.Int("blocks_skipped", skipped),
				zap.Duration("elapsed", elapsed),
				zap.Error(err),
			)
			return
		}
		if visited == 0 {
			prewarmFailedTotal.Inc()
			p.logger.Warn("prewarm walk completed but visited no blocks",
				zap.String("cid", cidStr),
				zap.Duration("elapsed", elapsed),
			)
			return
		}
		prewarmCompletedTotal.Inc()
		p.warmed.Store(cidStr, struct{}{})
		p.logger.Info("prewarm complete",
			zap.String("cid", cidStr),
			zap.Int("blocks_visited", visited),
			zap.Int("blocks_skipped", skipped),
			zap.Duration("elapsed", elapsed),
		)
	})
	if err != nil {
		p.mu.Lock()
		delete(p.active, cidStr)
		p.mu.Unlock()
		prewarmActiveWalks.Dec()
		p.logger.Debug("prewarm submission dropped", zap.String("cid", cidStr), zap.Error(err))
	}
}

// SubmitPath first resolves the given subpath through the DAG (caching
// just the blocks along that path), then triggers a full DAG walk via
// the same pool. If the subpath is empty, it delegates directly to
// Submit. If path resolution fails, the full walk still proceeds.
func (p *Prewarmer) SubmitPath(rootCID cid.Cid, subPath string) {
	_, span := otel.TraceMethod(p.ctx, "Prewarmer.SubmitPath",
		otel.WithAttributes(
			attribute.String("root_cid", rootCID.String()),
			attribute.String("sub_path", subPath),
		),
	)
	defer span.End()

	cidStr := rootCID.String()

	p.mu.Lock()
	if _, ok := p.active[cidStr]; ok {
		p.mu.Unlock()
		p.logger.Debug("prewarm path already in progress, skipping",
			zap.String("cid", cidStr),
			zap.String("sub_path", subPath),
		)
		return
	}
	if _, ok := p.warmed.Load(cidStr); ok {
		p.mu.Unlock()
		p.logger.Debug("prewarm already warmed, skipping",
			zap.String("cid", cidStr),
		)
		return
	}
	p.active[cidStr] = struct{}{}
	p.mu.Unlock()

	prewarmPathSubmittedTotal.Inc()
	prewarmActiveWalks.Inc()

	err := p.pool.Do(func() {
		defer func() {
			p.mu.Lock()
			delete(p.active, cidStr)
			p.mu.Unlock()
			prewarmActiveWalks.Dec()
		}()

		ctx, cancel := context.WithTimeout(p.ctx, p.timeout)
		defer cancel()

		// Phase 1: Resolve just the path blocks (best-effort)
		if subPath != "" {
			if err := p.resolvePathBlocks(ctx, rootCID, subPath); err != nil {
				p.logger.Debug("prewarm path resolution failed, continuing to full walk",
					zap.String("cid", cidStr),
					zap.String("sub_path", subPath),
					zap.Error(err),
				)
			}
		}

		// Phase 2: Full DAG walk
		visited, skipped, elapsed, walkErr := p.walk(ctx, rootCID)

		if walkErr != nil {
			prewarmFailedTotal.Inc()
			p.logger.Warn("prewarm walk failed",
				zap.String("cid", cidStr),
				zap.Int("blocks_visited", visited),
				zap.Int("blocks_skipped", skipped),
				zap.Duration("elapsed", elapsed),
				zap.Error(walkErr),
			)
			return
		}
		if visited == 0 {
			prewarmFailedTotal.Inc()
			p.logger.Warn("prewarm walk completed but visited no blocks",
				zap.String("cid", cidStr),
				zap.Duration("elapsed", elapsed),
			)
			return
		}
		p.warmed.Store(cidStr, struct{}{})
		prewarmCompletedTotal.Inc()
		p.logger.Info("prewarm complete",
			zap.String("cid", cidStr),
			zap.Int("blocks_visited", visited),
			zap.Int("blocks_skipped", skipped),
			zap.Duration("elapsed", elapsed),
		)
	})
	if err != nil {
		p.mu.Lock()
		delete(p.active, cidStr)
		p.mu.Unlock()
		prewarmActiveWalks.Dec()
		p.logger.Debug("prewarm path submission dropped", zap.String("cid", cidStr), zap.Error(err))
	}
}

// resolvePathBlocks walks just the DAG path from root to the target file,
// fetching intermediate directory nodes through the blockservice (which
// caches them in the ContentBlockstore).
func (p *Prewarmer) resolvePathBlocks(ctx context.Context, rootCID cid.Cid, subPath string) error {
	pathStr := "/ipfs/" + rootCID.String() + "/" + subPath
	pth, err := path.NewPath(pathStr)
	if err != nil {
		return fmt.Errorf("parse path %q: %w", pathStr, err)
	}
	immPath, err := path.NewImmutablePath(pth)
	if err != nil {
		return fmt.Errorf("immutable path %q: %w", pathStr, err)
	}

	// ResolveToLastNode walks all segments of the path, fetching each
	// intermediate block through the blockservice session.
	_, _, err = p.resolver.ResolveToLastNode(ctx, immPath)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", pathStr, err)
	}
	return nil
}

type walkStats struct {
	mu      sync.Mutex
	visited int
	skipped int
}

func (p *Prewarmer) walk(ctx context.Context, rootCID cid.Cid) (_ int, _ int, _ time.Duration, err error) {
	ctx, span := otel.TraceMethod(ctx, "Prewarmer.walk",
		otel.WithAttributes(attribute.String("root_cid", rootCID.String())),
	)
	defer func() { otel.EndSpanWithErr(span, err) }()

	dagSvc := merkledag.NewDAGService(p.bs)
	stats := &walkStats{}

	getLinks := func(ctx context.Context, c cid.Cid) ([]*format.Link, error) {
		var nd format.Node
		err := retry.New(
			retry.Context(ctx),
			retry.Attempts(p.retryAttempts),
			retry.Delay(p.retryDelay),
			retry.DelayType(retry.BackOffDelay),
			retry.LastErrorOnly(true),
		).Do(func() error {
			var err error
			nd, err = dagSvc.Get(ctx, c)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("fetch node %s: %w", c, err)
		}
		stats.mu.Lock()
		stats.visited++
		stats.mu.Unlock()
		prewarmBlocksFetched.Inc()
		return nd.Links(), nil
	}

	onError := func(c cid.Cid, err error) error {
		stats.mu.Lock()
		stats.skipped++
		stats.mu.Unlock()
		p.logger.Warn("prewarm: skipping unreachable block",
			zap.String("cid", c.String()),
			zap.Error(err),
		)
		return nil
	}

	start := time.Now()
	err = merkledag.Walk(ctx, getLinks, rootCID, func(cid.Cid) bool { return true },
		merkledag.Concurrent(),
		merkledag.OnError(onError),
	)
	elapsed := time.Since(start)

	return stats.visited, stats.skipped, elapsed, err
}

// IsWarmed returns true if a full DAG walk for the given CID has
// completed successfully. In-memory only; cleared when the block
// store cache evicts blocks via the eviction callback.
func (p *Prewarmer) IsWarmed(rootCID cid.Cid) bool {
	_, ok := p.warmed.Load(rootCID.String())
	return ok
}

// ClearWarmed removes a CID from the warmed set. Called by the
// ContentCache eviction callback when a block is evicted, so the
// next request to that site triggers a fresh prewarm walk.
func (p *Prewarmer) ClearWarmed(cidStr string) {
	p.warmed.Delete(cidStr)
}

func (p *Prewarmer) Stop() {
	p.pool.StopWait()
}
