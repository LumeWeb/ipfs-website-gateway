package cache

import (
	"context"
	"testing"

	"github.com/go-redis/redismock/v9"
	"github.com/stretchr/testify/require"
)

func newTestSSECursorStore(t *testing.T) (*SSECursorStore, redismock.ClientMock) {
	t.Helper()
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "gateway:")
	store := NewSSECursorStore(rc)
	return store, mock
}

func TestSSECursorStore_Get_Set(t *testing.T) {
	store, mock := newTestSSECursorStore(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:sse:cursor").SetVal("42")
	mark, err := store.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, 42, mark)

	mock.ExpectSet("gateway:sse:cursor", "43", 0).SetVal("OK")
	require.NoError(t, store.Set(ctx, 43))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSSECursorStore_Get_Missing(t *testing.T) {
	store, mock := newTestSSECursorStore(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:sse:cursor").RedisNil()
	mark, err := store.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, mark)
}

func TestSSECursorStore_Get_Invalid(t *testing.T) {
	store, mock := newTestSSECursorStore(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:sse:cursor").SetVal("not-an-int")
	_, err := store.Get(ctx)
	require.Error(t, err)
}

func TestSSECursorStore_NoRedis(t *testing.T) {
	store := NewSSECursorStore(nil)
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, 7))
	mark, err := store.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, 7, mark)
}
