// Package memstore is an in-memory bank.Store used by unit and handler tests.
//
// It mirrors the guarantees of the MySQL store: a transfer runs under a lock,
// and nothing is written unless the transfer callback succeeds. It uses one
// mutex for the whole store, which is coarser than MySQL's row locks but
// gives the same correctness.
package memstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

type Store struct {
	mu        sync.Mutex
	accounts  map[int64]bank.Account
	transfers []bank.Transfer
	ledger    []bank.LedgerEntry
	nextID    int64
}

// Compile-time check that *Store satisfies bank.Store. If a method is
// missing or has the wrong signature, this line fails to build.
var _ bank.Store = (*Store)(nil)

func New() *Store {
	return &Store{accounts: make(map[int64]bank.Account)}
}

func (s *Store) CreateAccount(_ context.Context, a bank.Account) (bank.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	a.ID = s.nextID
	s.accounts[a.ID] = a
	return a, nil
}

func (s *Store) GetAccount(_ context.Context, id int64) (bank.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[id]
	if !ok {
		return bank.Account{}, bank.ErrAccountNotFound
	}
	return a, nil
}

func (s *Store) ListLedger(_ context.Context, accountID int64, limit int) ([]bank.LedgerEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []bank.LedgerEntry
	for i := len(s.ledger) - 1; i >= 0 && len(out) < limit; i-- {
		if s.ledger[i].AccountID == accountID {
			out = append(out, s.ledger[i])
		}
	}
	return out, nil
}

func (s *Store) InTransferTx(_ context.Context, fromID, toID int64, fn func(bank.TransferTx, bank.Account, bank.Account) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	from, ok := s.accounts[fromID]
	if !ok {
		return fmt.Errorf("%w: %d", bank.ErrAccountNotFound, fromID)
	}
	to, ok := s.accounts[toID]
	if !ok {
		return fmt.Errorf("%w: %d", bank.ErrAccountNotFound, toID)
	}

	tx := &tx{s: s}
	if err := fn(tx, from, to); err != nil {
		return err // "rollback": staged writes are simply discarded
	}
	tx.commit()
	return nil
}

type tx struct {
	s      *Store
	staged *stagedTransfer
}

type stagedTransfer struct {
	transfer                         bank.Transfer
	fromBalanceAfter, toBalanceAfter money.Sen
}

func (t *tx) FindTransfer(_ context.Context, key string) (bank.Transfer, bool, error) {
	for _, tr := range t.s.transfers {
		if tr.IdempotencyKey == key {
			return tr, true, nil
		}
	}
	return bank.Transfer{}, false, nil
}

func (t *tx) SumDebitsSince(_ context.Context, accountID int64, since time.Time) (money.Sen, error) {
	var total money.Sen
	for _, tr := range t.s.transfers {
		if tr.FromAccountID == accountID && !tr.CreatedAt.Before(since) {
			total += tr.Amount
		}
	}
	return total, nil
}

func (t *tx) SaveTransfer(ctx context.Context, tr bank.Transfer, fromAfter, toAfter money.Sen) (bank.Transfer, error) {
	if _, dup, _ := t.FindTransfer(ctx, tr.IdempotencyKey); dup {
		return bank.Transfer{}, bank.ErrIdempotencyKeyReused
	}
	tr.ID = int64(len(t.s.transfers) + 1)
	t.staged = &stagedTransfer{transfer: tr, fromBalanceAfter: fromAfter, toBalanceAfter: toAfter}
	return tr, nil
}

func (t *tx) commit() {
	if t.staged == nil {
		return
	}
	tr := t.staged.transfer
	s := t.s

	from := s.accounts[tr.FromAccountID]
	from.Balance = t.staged.fromBalanceAfter
	s.accounts[from.ID] = from

	to := s.accounts[tr.ToAccountID]
	to.Balance = t.staged.toBalanceAfter
	s.accounts[to.ID] = to

	s.transfers = append(s.transfers, tr)
	s.ledger = append(s.ledger,
		bank.LedgerEntry{TransferID: tr.ID, AccountID: from.ID, CounterpartyAccountID: to.ID, Direction: bank.Debit,
			Amount: tr.Amount, BalanceAfter: from.Balance, Reference: tr.Reference, CreatedAt: tr.CreatedAt},
		bank.LedgerEntry{TransferID: tr.ID, AccountID: to.ID, CounterpartyAccountID: from.ID, Direction: bank.Credit,
			Amount: tr.Amount, BalanceAfter: to.Balance, Reference: tr.Reference, CreatedAt: tr.CreatedAt},
	)
}
