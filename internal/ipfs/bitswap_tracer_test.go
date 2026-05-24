package ipfs

import (
	"testing"

	"github.com/ipfs/boxo/bitswap/message"
	pb "github.com/ipfs/boxo/bitswap/message/pb"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func mustCreateCid(t *testing.T, data string) cid.Cid {
	t.Helper()
	h, err := mh.Sum([]byte(data), mh.SHA2_256, -1)
	if err != nil {
		t.Fatalf("multihash.Sum: %v", err)
	}
	return cid.NewCidV1(cid.Raw, h)
}

func TestDebugTracerMessageReceived(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	tracer := newDebugTracer(logger)

	pid := peer.ID("test-peer")
	msg := message.New(true)

	cid1 := mustCreateCid(t, "test data 1")
	msg.AddEntry(cid1, 10, pb.Message_Wantlist_Block, false)

	cid2 := mustCreateCid(t, "test data 2")
	msg.AddEntry(cid2, 5, pb.Message_Wantlist_Have, true)

	blk := blocks.NewBlock([]byte("block content"))
	msg.AddBlock(blk)

	msg.AddBlockPresence(mustCreateCid(t, "have data"), pb.Message_Have)
	msg.AddBlockPresence(mustCreateCid(t, "dont-have data"), pb.Message_DontHave)

	tracer.MessageReceived(pid, msg)
}

func TestDebugTracerMessageSent(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	tracer := newDebugTracer(logger)

	pid := peer.ID("test-peer")
	msg := message.New(true)

	cid1 := mustCreateCid(t, "want this")
	msg.AddEntry(cid1, 1, pb.Message_Wantlist_Block, false)

	tracer.MessageSent(pid, msg)
}

func TestDebugTracerSkipsEmptyMessage(t *testing.T) {
	called := false
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zaptest.NewTestingWriter(t),
		zap.DebugLevel,
	)
	core = &observeCore{Core: core, onWrite: func() { called = true }}
	logger := zap.New(core)

	tracer := newDebugTracer(logger)
	pid := peer.ID("test-peer")
	msg := message.New(true)

	tracer.MessageReceived(pid, msg)

	if called {
		t.Error("empty message should not produce a log entry")
	}
}

func TestDebugTracerCancelEntry(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	tracer := newDebugTracer(logger)

	pid := peer.ID("test-peer")
	msg := message.New(true)
	msg.Cancel(mustCreateCid(t, "cancel this"))

	tracer.MessageReceived(pid, msg)
}

type observeCore struct {
	zapcore.Core
	onWrite func()
}

func (o *observeCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	o.onWrite()
	return o.Core.Write(ent, fields)
}
