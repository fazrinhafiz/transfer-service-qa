// Package bank holds the domain model and business rules for accounts and
// fund transfers. It knows nothing about HTTP, MySQL or Redis: storage is
// reached through the Store interface, which keeps the rules unit-testable.
package bank

import (
	"time"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

type Account struct {
	ID        int64
	OwnerName string
	Balance   money.Sen
	CreatedAt time.Time
}

type Transfer struct {
	ID             int64
	IdempotencyKey string
	FromAccountID  int64
	ToAccountID    int64
	Amount         money.Sen
	Reference      string
	CreatedAt      time.Time
}

// matches reports whether a stored transfer was created from the same
// request, so a retried idempotency key can be told apart from a reused one.
func (t Transfer) matches(req TransferRequest) bool {
	return t.FromAccountID == req.FromAccountID &&
		t.ToAccountID == req.ToAccountID &&
		t.Amount == req.Amount &&
		t.Reference == req.Reference
}

type Direction string

const (
	Debit  Direction = "DEBIT"
	Credit Direction = "CREDIT"
)

// LedgerEntry is one line of an account's transaction history. Every
// transfer writes two entries: a debit on the sender and a credit on the
// receiver (double-entry bookkeeping).
type LedgerEntry struct {
	TransferID            int64
	AccountID             int64
	CounterpartyAccountID int64
	Direction             Direction
	Amount                money.Sen
	BalanceAfter          money.Sen
	Reference             string
	CreatedAt             time.Time
}

type CreateAccountRequest struct {
	OwnerName      string
	InitialBalance money.Sen
}

type TransferRequest struct {
	IdempotencyKey string
	FromAccountID  int64
	ToAccountID    int64
	Amount         money.Sen
	Reference      string
}
