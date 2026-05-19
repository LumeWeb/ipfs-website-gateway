package ipfs

import (
	"context"
	"io"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	ds "github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
)

type seedPeerRouting struct {
	dht    *dht.IpfsDHT
	seedID peer.ID
}

var _ routing.ValueStore = (*seedPeerRouting)(nil)

func newSeedPeerRouting(ctx context.Context, h host.Host, seedID peer.ID) (*seedPeerRouting, error) {
	dstore := dssync.MutexWrap(ds.NewMapDatastore())

	d := dht.NewDHTClient(ctx, h, dstore)

	return &seedPeerRouting{
		dht:    d,
		seedID: seedID,
	}, nil
}

func (s *seedPeerRouting) PutValue(ctx context.Context, key string, value []byte, opts ...routing.Option) error {
	return routing.ErrNotSupported
}

func (s *seedPeerRouting) GetValue(ctx context.Context, key string, opts ...routing.Option) ([]byte, error) {
	if len(s.seedID) == 0 {
		return nil, routing.ErrNotFound
	}

	return s.dht.GetValue(ctx, key, opts...)
}

func (s *seedPeerRouting) SearchValue(ctx context.Context, key string, opts ...routing.Option) (<-chan []byte, error) {
	if len(s.seedID) == 0 {
		ch := make(chan []byte)
		close(ch)
		return ch, routing.ErrNotFound
	}

	return s.dht.SearchValue(ctx, key, opts...)
}

func (s *seedPeerRouting) Close() error {
	return s.dht.Close()
}

var _ io.Closer = (*seedPeerRouting)(nil)
