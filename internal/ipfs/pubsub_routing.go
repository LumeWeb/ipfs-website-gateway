package ipfs

import (
	"context"

	"github.com/libp2p/go-libp2p/core/routing"
)

type pubsubFirstRouting struct {
	pubsub routing.ValueStore
	dht    routing.ValueStore
}

var _ routing.ValueStore = (*pubsubFirstRouting)(nil)

func newPubsubFirstRouting(pubsub, dht routing.ValueStore) *pubsubFirstRouting {
	return &pubsubFirstRouting{pubsub: pubsub, dht: dht}
}

func (r *pubsubFirstRouting) PutValue(context.Context, string, []byte, ...routing.Option) error {
	return routing.ErrNotSupported
}

func (r *pubsubFirstRouting) GetValue(ctx context.Context, key string, opts ...routing.Option) ([]byte, error) {
	val, err := r.pubsub.GetValue(ctx, key, opts...)
	if err == nil {
		return val, nil
	}
	return r.dht.GetValue(ctx, key, opts...)
}

func (r *pubsubFirstRouting) SearchValue(ctx context.Context, key string, opts ...routing.Option) (<-chan []byte, error) {
	ch, err := r.pubsub.SearchValue(ctx, key, opts...)
	if err == nil {
		return ch, nil
	}
	return r.dht.SearchValue(ctx, key, opts...)
}
