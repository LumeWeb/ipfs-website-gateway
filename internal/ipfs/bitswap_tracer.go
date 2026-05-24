package ipfs

import (
	"context"
	"fmt"
	"io"
	"time"

	bsmsg "github.com/ipfs/boxo/bitswap/message"
	"github.com/ipfs/boxo/bitswap/client/traceability"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/exchange"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

// debugTracer implements tracer.Tracer to log Bitswap messages at debug level.
// Only emits logs when the logger is at debug level — zero overhead otherwise
// since zap.Debug elides the allocation when the level is disabled.
type debugTracer struct {
	logger *zap.Logger
}

func newDebugTracer(logger *zap.Logger) *debugTracer {
	return &debugTracer{logger: logger.Named("bitswap")}
}

func (t *debugTracer) MessageReceived(p peer.ID, msg bsmsg.BitSwapMessage) {
	wantlist := msg.Wantlist()
	blocks := msg.Blocks()
	haves := msg.Haves()
	dontHaves := msg.DontHaves()

	if len(wantlist) == 0 && len(blocks) == 0 && len(haves) == 0 && len(dontHaves) == 0 {
		return
	}

	fields := []zap.Field{
		zap.String("peer", p.String()),
		zap.Int("wantlist_entries", len(wantlist)),
		zap.Int("blocks", len(blocks)),
		zap.Int("haves", len(haves)),
		zap.Int("dont_haves", len(dontHaves)),
	}

	for _, b := range blocks {
		fields = append(fields,
			zap.String("block_cid", b.Cid().String()),
			zap.Int("block_size", len(b.RawData())),
		)
	}

	if len(haves) > 0 {
		cids := make([]string, 0, len(haves))
		for _, c := range haves {
			cids = append(cids, c.String())
		}
		fields = append(fields, zap.Strings("have_cids", cids))
	}

	for _, e := range wantlist {
		action := "want"
		if e.Cancel {
			action = "cancel"
		}
		fields = append(fields,
			zap.String(fmt.Sprintf("wantlist_%s", action), fmt.Sprintf("%s (%s)", e.Cid, e.WantType)),
		)
	}

	t.logger.Debug("message received", fields...)
}

func (t *debugTracer) MessageSent(p peer.ID, msg bsmsg.BitSwapMessage) {
	wantlist := msg.Wantlist()
	blocks := msg.Blocks()
	haves := msg.Haves()
	dontHaves := msg.DontHaves()

	if len(wantlist) == 0 && len(blocks) == 0 && len(haves) == 0 && len(dontHaves) == 0 {
		return
	}

	fields := []zap.Field{
		zap.String("peer", p.String()),
		zap.Int("wantlist_entries", len(wantlist)),
		zap.Int("blocks", len(blocks)),
		zap.Int("haves", len(haves)),
		zap.Int("dont_haves", len(dontHaves)),
	}

	for _, e := range wantlist {
		action := "want"
		if e.Cancel {
			action = "cancel"
		}
		fields = append(fields,
			zap.String(fmt.Sprintf("wantlist_%s", action), fmt.Sprintf("%s (%s)", e.Cid, e.WantType)),
		)
	}

	t.logger.Debug("message sent", fields...)
}

type loggingBlockService struct {
	blockservice.BlockService
	logger *zap.Logger
}

func newLoggingBlockService(bs blockservice.BlockService, logger *zap.Logger) *loggingBlockService {
	return &loggingBlockService{BlockService: bs, logger: logger.Named("blockservice")}
}

func (s *loggingBlockService) GetBlock(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	start := time.Now()
	blk, err := s.BlockService.GetBlock(ctx, c)
	elapsed := time.Since(start)

	if err != nil {
		s.logger.Debug("get block failed",
			zap.String("cid", c.String()),
			zap.Duration("elapsed", elapsed),
			zap.Error(err),
		)
		return nil, err
	}

	fields := []zap.Field{
		zap.String("cid", c.String()),
		zap.Int("size", len(blk.RawData())),
		zap.Duration("elapsed", elapsed),
	}

	if tb, ok := blk.(*traceability.Block); ok && tb.From != "" {
		fields = append(fields,
			zap.String("from_peer", tb.From.String()),
			zap.Duration("bitswap_delay", tb.Delay),
		)
	}

	s.logger.Debug("get block", fields...)
	return blk, nil
}

func (s *loggingBlockService) GetBlocks(ctx context.Context, ks []cid.Cid) <-chan blocks.Block {
	if len(ks) == 0 {
		return s.BlockService.GetBlocks(ctx, ks)
	}

	s.logger.Debug("get blocks batch",
		zap.Int("count", len(ks)),
	)

	ch := s.BlockService.GetBlocks(ctx, ks)
	out := make(chan blocks.Block, len(ks))

	go func() {
		defer close(out)
		for blk := range ch {
			fields := []zap.Field{
				zap.String("cid", blk.Cid().String()),
				zap.Int("size", len(blk.RawData())),
			}

			if tb, ok := blk.(*traceability.Block); ok && tb.From != "" {
				fields = append(fields,
					zap.String("from_peer", tb.From.String()),
					zap.Duration("bitswap_delay", tb.Delay),
				)
			}

			s.logger.Debug("block received (batch)", fields...)
			out <- blk
		}
	}()

	return out
}

func (s *loggingBlockService) Close() error {
	if closer, ok := s.BlockService.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *loggingBlockService) Blockstore() blockstore.Blockstore {
	return s.BlockService.Blockstore()
}

func (s *loggingBlockService) Exchange() exchange.Interface {
	return s.BlockService.Exchange()
}

func (s *loggingBlockService) AddBlock(ctx context.Context, o blocks.Block) error {
	return s.BlockService.AddBlock(ctx, o)
}

func (s *loggingBlockService) AddBlocks(ctx context.Context, bs []blocks.Block) error {
	return s.BlockService.AddBlocks(ctx, bs)
}

func (s *loggingBlockService) DeleteBlock(ctx context.Context, o cid.Cid) error {
	return s.BlockService.DeleteBlock(ctx, o)
}
