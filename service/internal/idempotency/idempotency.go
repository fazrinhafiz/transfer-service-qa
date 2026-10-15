// Package idempotency remembers the response to each Idempotency-Key so a
// retried request gets the original result instead of being executed again.
//
// Lifecycle of a key:
//
//	Begin    -> reserves the key as "in progress" (short TTL, in case we crash)
//	Complete -> stores the final response (long TTL)
//	Release  -> deletes the reservation after an unexpected error so the
//	            client can safely retry
package idempotency

import (
	"context"
	"time"
)

const (
	// PendingTTL bounds how long a crashed request can block its key.
	PendingTTL = 30 * time.Second
	// CompletedTTL is how long a finished response can be replayed.
	CompletedTTL = 24 * time.Hour
)

// Record is what is stored against a key.
type Record struct {
	// Fingerprint identifies the request body, so reusing a key for a
	// different request can be detected.
	Fingerprint string `json:"fingerprint"`
	Completed   bool   `json:"completed"`
	StatusCode  int    `json:"status_code,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

type Store interface {
	// Begin atomically reserves key. If the key was free it returns
	// reserved=true. Otherwise it returns the existing record.
	Begin(ctx context.Context, key, fingerprint string) (existing Record, reserved bool, err error)
	Complete(ctx context.Context, key string, rec Record) error
	Release(ctx context.Context, key string) error
}
