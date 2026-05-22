package ipfs

import (
	"context"
	"errors"
	"testing"

	"github.com/libp2p/go-libp2p/core/routing"
)

type mockValueStore struct {
	getValueFn    func(ctx context.Context, key string, opts ...routing.Option) ([]byte, error)
	searchValueFn func(ctx context.Context, key string, opts ...routing.Option) (<-chan []byte, error)
}

var _ routing.ValueStore = (*mockValueStore)(nil)

func (m *mockValueStore) PutValue(context.Context, string, []byte, ...routing.Option) error {
	return routing.ErrNotSupported
}

func (m *mockValueStore) GetValue(ctx context.Context, key string, opts ...routing.Option) ([]byte, error) {
	if m.getValueFn != nil {
		return m.getValueFn(ctx, key, opts...)
	}
	return nil, nil
}

func (m *mockValueStore) SearchValue(ctx context.Context, key string, opts ...routing.Option) (<-chan []byte, error) {
	if m.searchValueFn != nil {
		return m.searchValueFn(ctx, key, opts...)
	}
	ch := make(chan []byte)
	close(ch)
	return ch, nil
}

func TestPubsubFirstRouting_PutValue_NotSupported(t *testing.T) {
	r := newPubsubFirstRouting(nil, nil)
	err := r.PutValue(context.Background(), "key", []byte("val"))
	if err != routing.ErrNotSupported {
		t.Fatalf("got %v, want ErrNotSupported", err)
	}
}

func TestPubsubFirstRouting_GetValue_PrefersPubsub(t *testing.T) {
	pubsub := &mockValueStore{
		getValueFn: func(_ context.Context, _ string, _ ...routing.Option) ([]byte, error) {
			return []byte("from-pubsub"), nil
		},
	}

	dht := &mockValueStore{
		getValueFn: func(_ context.Context, _ string, _ ...routing.Option) ([]byte, error) {
			return []byte("from-dht"), nil
		},
	}

	r := newPubsubFirstRouting(pubsub, dht)
	val, err := r.GetValue(context.Background(), "key")
	if err != nil {
		t.Fatalf("GetValue returned error: %v", err)
	}
	if string(val) != "from-pubsub" {
		t.Fatalf("got %q, want %q", val, "from-pubsub")
	}
}

func TestPubsubFirstRouting_GetValue_FallsBackToDHT(t *testing.T) {
	pubsub := &mockValueStore{
		getValueFn: func(_ context.Context, _ string, _ ...routing.Option) ([]byte, error) {
			return nil, errors.New("not found")
		},
	}

	dht := &mockValueStore{
		getValueFn: func(_ context.Context, _ string, _ ...routing.Option) ([]byte, error) {
			return []byte("from-dht"), nil
		},
	}

	r := newPubsubFirstRouting(pubsub, dht)
	val, err := r.GetValue(context.Background(), "key")
	if err != nil {
		t.Fatalf("GetValue returned error: %v", err)
	}
	if string(val) != "from-dht" {
		t.Fatalf("got %q, want %q", val, "from-dht")
	}
}

func TestPubsubFirstRouting_GetValue_BothFail(t *testing.T) {
	dhtErr := errors.New("dht failed")
	pubsubErr := errors.New("pubsub failed")

	pubsub := &mockValueStore{
		getValueFn: func(_ context.Context, _ string, _ ...routing.Option) ([]byte, error) {
			return nil, pubsubErr
		},
	}

	dht := &mockValueStore{
		getValueFn: func(_ context.Context, _ string, _ ...routing.Option) ([]byte, error) {
			return nil, dhtErr
		},
	}

	r := newPubsubFirstRouting(pubsub, dht)
	_, err := r.GetValue(context.Background(), "key")
	if err != dhtErr {
		t.Fatalf("got %v, want %v", err, dhtErr)
	}
}

func TestPubsubFirstRouting_SearchValue_PrefersPubsub(t *testing.T) {
	pubsubCh := make(chan []byte, 1)
	pubsubCh <- []byte("from-pubsub")

	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return pubsubCh, nil
		},
	}

	dhtCh := make(chan []byte, 1)
	dhtCh <- []byte("from-dht")

	dht := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return dhtCh, nil
		},
	}

	r := newPubsubFirstRouting(pubsub, dht)
	ch, err := r.SearchValue(context.Background(), "key")
	if err != nil {
		t.Fatalf("SearchValue returned error: %v", err)
	}
	val := <-ch
	if string(val) != "from-pubsub" {
		t.Fatalf("got %q, want %q", val, "from-pubsub")
	}
}

func TestPubsubFirstRouting_SearchValue_FallsBackToDHT(t *testing.T) {
	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return nil, errors.New("not found")
		},
	}

	dhtCh := make(chan []byte, 1)
	dhtCh <- []byte("from-dht")

	dht := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return dhtCh, nil
		},
	}

	r := newPubsubFirstRouting(pubsub, dht)
	ch, err := r.SearchValue(context.Background(), "key")
	if err != nil {
		t.Fatalf("SearchValue returned error: %v", err)
	}
	val := <-ch
	if string(val) != "from-dht" {
		t.Fatalf("got %q, want %q", val, "from-dht")
	}
}
