package ipfs

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestPubsubFirstRouting_SearchValue_PubsubErrors_DHTDelivers(t *testing.T) {
	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return nil, errors.New("pubsub not available")
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

func TestPubsubFirstRouting_SearchValue_DHTErrors_PubsubDelivers(t *testing.T) {
	pubsubCh := make(chan []byte, 1)
	pubsubCh <- []byte("from-pubsub")

	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return pubsubCh, nil
		},
	}

	dht := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return nil, errors.New("dht not available")
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

func TestPubsubFirstRouting_SearchValue_BothError(t *testing.T) {
	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return nil, errors.New("pubsub failed")
		},
	}

	dht := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return nil, errors.New("dht failed")
		},
	}

	r := newPubsubFirstRouting(pubsub, dht)
	_, err := r.SearchValue(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPubsubFirstRouting_SearchValue_PubsubEmpty_DHTDelivers(t *testing.T) {
	pubsubCh := make(chan []byte)

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
	if string(val) != "from-dht" {
		t.Fatalf("got %q, want %q", val, "from-dht")
	}
}

func TestPubsubFirstRouting_SearchValue_PubsubSlow_DHTWins(t *testing.T) {
	pubsubCh := make(chan []byte)

	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return pubsubCh, nil
		},
	}

	dhtCh := make(chan []byte, 1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		dhtCh <- []byte("from-dht")
	}()

	dht := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return dhtCh, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := newPubsubFirstRouting(pubsub, dht)
	ch, err := r.SearchValue(ctx, "key")
	if err != nil {
		t.Fatalf("SearchValue returned error: %v", err)
	}
	val := <-ch
	if string(val) != "from-dht" {
		t.Fatalf("got %q, want %q", val, "from-dht")
	}
}

func TestPubsubFirstRouting_SearchValue_PubsubFast_PubsubWins(t *testing.T) {
	pubsubCh := make(chan []byte, 1)
	pubsubCh <- []byte("from-pubsub")

	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return pubsubCh, nil
		},
	}

	dhtCh := make(chan []byte)

	dht := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return dhtCh, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := newPubsubFirstRouting(pubsub, dht)
	ch, err := r.SearchValue(ctx, "key")
	if err != nil {
		t.Fatalf("SearchValue returned error: %v", err)
	}
	val := <-ch
	if string(val) != "from-pubsub" {
		t.Fatalf("got %q, want %q", val, "from-pubsub")
	}
}

func TestPubsubFirstRouting_SearchValue_BothEmpty_Timeout(t *testing.T) {
	pubsubCh := make(chan []byte)

	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return pubsubCh, nil
		},
	}

	dhtCh := make(chan []byte)

	dht := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return dhtCh, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := newPubsubFirstRouting(pubsub, dht)
	ch, err := r.SearchValue(ctx, "key")
	if err != nil {
		t.Fatalf("SearchValue returned error: %v", err)
	}

	_, ok := <-ch
	if ok {
		t.Error("expected channel to close on context timeout without data")
	}
}

func TestPubsubFirstRouting_SearchValue_DrainsLoserChannel(t *testing.T) {
	pubsubCh := make(chan []byte, 1)
	pubsubCh <- []byte("from-pubsub")

	pubsub := &mockValueStore{
		searchValueFn: func(_ context.Context, _ string, _ ...routing.Option) (<-chan []byte, error) {
			return pubsubCh, nil
		},
	}

	dhtCh := make(chan []byte, 3)

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

	dhtCh <- []byte("dht-1")
	dhtCh <- []byte("dht-2")
	close(dhtCh)

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("losing channel was not drained — goroutine leak")
	}
}
