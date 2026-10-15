package idempotency_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/idempotency"
)

// newRedisStore starts an in-process fake Redis server for one test.
func newRedisStore(t *testing.T) (*idempotency.RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t) // stopped automatically when the test ends
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return idempotency.NewRedisStore(rdb), mr
}

func TestRedisStore_FirstBeginReservesKey(t *testing.T) {
	store, mr := newRedisStore(t)

	_, reserved, err := store.Begin(context.Background(), "k1", "fp-a")

	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, idempotency.PendingTTL, mr.TTL("idempotency:k1"))
}

func TestRedisStore_SecondBeginSeesPendingRecord(t *testing.T) {
	store, _ := newRedisStore(t)
	ctx := context.Background()
	_, _, err := store.Begin(ctx, "k1", "fp-a")
	require.NoError(t, err)

	rec, reserved, err := store.Begin(ctx, "k1", "fp-a")

	require.NoError(t, err)
	assert.False(t, reserved)
	assert.False(t, rec.Completed)
	assert.Equal(t, "fp-a", rec.Fingerprint)
}

func TestRedisStore_CompletedRecordIsReplayed(t *testing.T) {
	store, mr := newRedisStore(t)
	ctx := context.Background()
	_, _, err := store.Begin(ctx, "k1", "fp-a")
	require.NoError(t, err)

	err = store.Complete(ctx, "k1", idempotency.Record{Fingerprint: "fp-a", StatusCode: 201, Body: []byte(`{"id":7}`)})
	require.NoError(t, err)

	rec, reserved, err := store.Begin(ctx, "k1", "fp-a")
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.True(t, rec.Completed)
	assert.Equal(t, 201, rec.StatusCode)
	assert.JSONEq(t, `{"id":7}`, string(rec.Body))
	assert.Equal(t, idempotency.CompletedTTL, mr.TTL("idempotency:k1"))
}

func TestRedisStore_ReleaseFreesKey(t *testing.T) {
	store, _ := newRedisStore(t)
	ctx := context.Background()
	_, _, err := store.Begin(ctx, "k1", "fp-a")
	require.NoError(t, err)

	require.NoError(t, store.Release(ctx, "k1"))

	_, reserved, err := store.Begin(ctx, "k1", "fp-a")
	require.NoError(t, err)
	assert.True(t, reserved)
}

// A request that crashes mid-flight must not block its key forever.
func TestRedisStore_PendingReservationExpires(t *testing.T) {
	store, mr := newRedisStore(t)
	ctx := context.Background()
	_, _, err := store.Begin(ctx, "k1", "fp-a")
	require.NoError(t, err)

	mr.FastForward(idempotency.PendingTTL + time.Second)

	_, reserved, err := store.Begin(ctx, "k1", "fp-a")
	require.NoError(t, err)
	assert.True(t, reserved)
}

func TestRedisStore_ReturnsErrorWhenRedisIsDown(t *testing.T) {
	store, mr := newRedisStore(t)
	mr.Close()

	_, _, err := store.Begin(context.Background(), "k1", "fp-a")

	assert.Error(t, err)
}
