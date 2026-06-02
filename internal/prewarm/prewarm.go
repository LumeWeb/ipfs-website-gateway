package prewarm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/gammazero/workerpool"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/ipld/merkledag"
	cid "github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type Prewarmer struct {
	bs            blockservice.BlockService
	logger        *zap.Logger
	timeout       time.Duration
	retryAttempts uint
	retryDelay    time.Duration
	ctx           context.Context

	mu     sync.Mutex
	active map[string]struct{}
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
	return &Prewarmer{
		bs:            bs,
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
		p.logger.Debug("prewarm submission dropped", zap.String("cid", cidStr), zap.Error(err))
	}
}

type walkStats struct {
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
		stats.visited++
		prewarmBlocksFetched.Inc()
		return nd.Links(), nil
	}

	onError := func(c cid.Cid, err error) error {
		stats.skipped++
		p.logger.Warn("prewarm: skipping unreachable block",
			zap.String("cid", c.String()),
			zap.Error(err),
		)
		return nil
	}

	start := time.Now()
	err = merkledag.Walk(ctx, getLinks, rootCID, func(cid.Cid) bool { return true },
		merkledag.OnError(onError),
	)
	elapsed := time.Since(start)

	return stats.visited, stats.skipped, elapsed, err
}

func (p *Prewarmer) Stop() {
	p.pool.StopWait()
}
