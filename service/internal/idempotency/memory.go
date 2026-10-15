package idempotency

import (
	"context"
	"sync"
)

// MemoryStore is an in-process Store for tests. It does not expire keys.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]Record)}
}

func (s *MemoryStore) Begin(_ context.Context, key, fingerprint string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.records[key]; ok {
		return rec, false, nil
	}
	s.records[key] = Record{Fingerprint: fingerprint}
	return Record{}, true, nil
}

func (s *MemoryStore) Complete(_ context.Context, key string, rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.Completed = true
	s.records[key] = rec
	return nil
}

func (s *MemoryStore) Release(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, key)
	return nil
}
