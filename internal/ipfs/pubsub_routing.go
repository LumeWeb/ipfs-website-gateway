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
	psCh, psErr := r.pubsub.SearchValue(ctx, key, opts...)
	dhtCh, dhtErr := r.dht.SearchValue(ctx, key, opts...)

	if psErr != nil && dhtErr != nil {
		return nil, dhtErr
	}

	if psErr != nil {
		return dhtCh, nil
	}

	if dhtErr != nil {
		return psCh, nil
	}

	out := make(chan []byte, 1)

	go func() {
		defer close(out)

		for psCh != nil || dhtCh != nil {
			select {
			case val, ok := <-psCh:
				if ok {
					out <- val
					go drainChannel(dhtCh)
					return
				}
				psCh = nil
			case val, ok := <-dhtCh:
				if ok {
					out <- val
					go drainChannel(psCh)
					return
				}
				dhtCh = nil
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

func drainChannel(ch <-chan []byte) {
	if ch == nil {
		return
	}
	for range ch {
	}
}
