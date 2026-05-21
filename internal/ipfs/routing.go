package ipfs

import (
	"context"
	"fmt"
	"io"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/routing"
	ds "github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
)

type seedPeerRouting struct {
	dht *dht.IpfsDHT
}

var _ routing.ValueStore = (*seedPeerRouting)(nil)

func newSeedPeerRouting(ctx context.Context, h host.Host) (*seedPeerRouting, error) {
	dstore := dssync.MutexWrap(ds.NewMapDatastore())

	d := dht.NewDHTClient(ctx, h, dstore)

	return &seedPeerRouting{
		dht: d,
	}, nil
}

func (s *seedPeerRouting) Bootstrap(ctx context.Context) error {
	if err := s.dht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT client: %w", err)
	}
	return nil
}

func (s *seedPeerRouting) PutValue(ctx context.Context, key string, value []byte, opts ...routing.Option) error {
	return routing.ErrNotSupported
}

func (s *seedPeerRouting) GetValue(ctx context.Context, key string, opts ...routing.Option) ([]byte, error) {
	return s.dht.GetValue(ctx, key, opts...)
}

func (s *seedPeerRouting) SearchValue(ctx context.Context, key string, opts ...routing.Option) (<-chan []byte, error) {
	return s.dht.SearchValue(ctx, key, opts...)
}

func (s *seedPeerRouting) Close() error {
	return s.dht.Close()
}

var _ io.Closer = (*seedPeerRouting)(nil)
