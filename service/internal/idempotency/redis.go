package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "idempotency:"

type RedisStore struct {
	rdb *redis.Client
}

var _ Store = (*RedisStore)(nil)

func NewRedisStore(rdb *redis.Client) *RedisStore {
	return &RedisStore{rdb: rdb}
}

func (s *RedisStore) Begin(ctx context.Context, key, fingerprint string) (Record, bool, error) {
	pending, err := json.Marshal(Record{Fingerprint: fingerprint})
	if err != nil {
		return Record{}, false, err
	}

	// SET key value NX: only set if the key does not exist. Redis runs each
	// command atomically, so when two identical requests race, exactly one
	// of them wins the reservation.
	err = s.rdb.SetArgs(ctx, keyPrefix+key, pending, redis.SetArgs{Mode: "NX", TTL: PendingTTL}).Err()
	if err == nil {
		return Record{}, true, nil
	}
	if !errors.Is(err, redis.Nil) { // redis.Nil means "not set because it exists"
		return Record{}, false, fmt.Errorf("reserve idempotency key: %w", err)
	}

	raw, err := s.rdb.Get(ctx, keyPrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		// The holder released or expired the key between our SET and GET.
		// Reporting "in progress" is safe: the client simply retries.
		return Record{Fingerprint: fingerprint}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read idempotency key: %w", err)
	}

	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Record{}, false, fmt.Errorf("decode idempotency record: %w", err)
	}
	return rec, false, nil
}

func (s *RedisStore) Complete(ctx context.Context, key string, rec Record) error {
	rec.Completed = true
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, keyPrefix+key, raw, CompletedTTL).Err()
}

func (s *RedisStore) Release(ctx context.Context, key string) error {
	return s.rdb.Del(ctx, keyPrefix+key).Err()
}
