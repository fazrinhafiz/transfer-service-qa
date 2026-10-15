package bank

import (
	"context"
	"time"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

// Store is the persistence port the service depends on. In Go, a type
// satisfies an interface just by having the methods; there is no
// "implements" keyword. Both mysqlstore and memstore satisfy Store.
type Store interface {
	CreateAccount(ctx context.Context, a Account) (Account, error)
	GetAccount(ctx context.Context, id int64) (Account, error)
	ListLedger(ctx context.Context, accountID int64, limit int) ([]LedgerEntry, error)

	// InTransferTx locks both accounts and runs fn inside one database
	// transaction. If fn returns an error, nothing is written. The store
	// must lock accounts in a consistent order (lowest id first) so two
	// opposite transfers (A->B and B->A) cannot deadlock.
	//
	// Returns ErrAccountNotFound if either account does not exist.
	InTransferTx(ctx context.Context, fromID, toID int64, fn func(tx TransferTx, from, to Account) error) error
}

// TransferTx is the set of operations available while both accounts are locked.
type TransferTx interface {
	// FindTransfer returns the transfer created with key, and false if there is none.
	FindTransfer(ctx context.Context, idempotencyKey string) (Transfer, bool, error)

	// SumDebitsSince totals the transfers sent from accountID at or after since.
	SumDebitsSince(ctx context.Context, accountID int64, since time.Time) (money.Sen, error)

	// SaveTransfer records t, sets both new balances and writes the ledger
	// entries. It returns t with its ID set. A duplicate idempotency key
	// returns ErrIdempotencyKeyReused.
	SaveTransfer(ctx context.Context, t Transfer, fromBalanceAfter, toBalanceAfter money.Sen) (Transfer, error)
}
