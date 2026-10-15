// Package mysqlstore implements bank.Store on MySQL (InnoDB).
package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

// MySQL error 1062 is ER_DUP_ENTRY: a unique index was violated.
const errDuplicateEntry = 1062

type Store struct {
	db *sql.DB
}

var _ bank.Store = (*Store)(nil)

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateAccount(ctx context.Context, a bank.Account) (bank.Account, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO accounts (owner_name, balance_sen, created_at) VALUES (?, ?, ?)`,
		a.OwnerName, a.Balance, a.CreatedAt)
	if err != nil {
		return bank.Account{}, fmt.Errorf("insert account: %w", err)
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return bank.Account{}, fmt.Errorf("account id: %w", err)
	}
	return a, nil
}

func (s *Store) GetAccount(ctx context.Context, id int64) (bank.Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx,
		`SELECT id, owner_name, balance_sen, created_at FROM accounts WHERE id = ?`, id))
}

func (s *Store) ListLedger(ctx context.Context, accountID int64, limit int) ([]bank.LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT transfer_id, account_id, counterparty_account_id, direction,
		       amount_sen, balance_after_sen, reference, created_at
		FROM ledger_entries
		WHERE account_id = ?
		ORDER BY id DESC
		LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("query ledger: %w", err)
	}
	// defer runs when the function returns, so the rows are always closed,
	// even on an early error return below.
	defer rows.Close()

	entries := []bank.LedgerEntry{}
	for rows.Next() {
		var e bank.LedgerEntry
		if err := rows.Scan(&e.TransferID, &e.AccountID, &e.CounterpartyAccountID, &e.Direction,
			&e.Amount, &e.BalanceAfter, &e.Reference, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) InTransferTx(ctx context.Context, fromID, toID int64, fn func(bank.TransferTx, bank.Account, bank.Account) error) (err error) {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Roll back on any failure. After a successful Commit, Rollback is a
	// harmless no-op, so this deferred call is always safe.
	defer func() {
		if err != nil {
			_ = sqlTx.Rollback()
		}
	}()

	// Lock rows in ascending id order. If one transfer locks A then B while
	// another locks B then A, each can wait on the other forever (deadlock).
	// A global ordering makes that impossible.
	first, second := fromID, toID
	if first > second {
		first, second = second, first
	}
	locked := make(map[int64]bank.Account, 2)
	for _, id := range []int64{first, second} {
		a, err := scanAccount(sqlTx.QueryRowContext(ctx,
			`SELECT id, owner_name, balance_sen, created_at FROM accounts WHERE id = ? FOR UPDATE`, id))
		if err != nil {
			if errors.Is(err, bank.ErrAccountNotFound) {
				return fmt.Errorf("%w: %d", bank.ErrAccountNotFound, id)
			}
			return err
		}
		locked[id] = a
	}

	if err := fn(&tx{tx: sqlTx}, locked[fromID], locked[toID]); err != nil {
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

type tx struct {
	tx *sql.Tx
}

func (t *tx) FindTransfer(ctx context.Context, key string) (bank.Transfer, bool, error) {
	var tr bank.Transfer
	err := t.tx.QueryRowContext(ctx, `
		SELECT id, idempotency_key, from_account_id, to_account_id, amount_sen, reference, created_at
		FROM transfers WHERE idempotency_key = ?`, key).
		Scan(&tr.ID, &tr.IdempotencyKey, &tr.FromAccountID, &tr.ToAccountID, &tr.Amount, &tr.Reference, &tr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return bank.Transfer{}, false, nil
	}
	if err != nil {
		return bank.Transfer{}, false, fmt.Errorf("find transfer: %w", err)
	}
	return tr, true, nil
}

func (t *tx) SumDebitsSince(ctx context.Context, accountID int64, since time.Time) (money.Sen, error) {
	var total money.Sen
	err := t.tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount_sen), 0) FROM transfers
		WHERE from_account_id = ? AND created_at >= ?`, accountID, since).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum debits: %w", err)
	}
	return total, nil
}

func (t *tx) SaveTransfer(ctx context.Context, tr bank.Transfer, fromAfter, toAfter money.Sen) (bank.Transfer, error) {
	res, err := t.tx.ExecContext(ctx, `
		INSERT INTO transfers (idempotency_key, from_account_id, to_account_id, amount_sen, reference, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		tr.IdempotencyKey, tr.FromAccountID, tr.ToAccountID, tr.Amount, tr.Reference, tr.CreatedAt)
	if err != nil {
		// errors.As checks whether err (or anything it wraps) is a
		// *mysql.MySQLError, and if so assigns it to myErr.
		var myErr *mysql.MySQLError
		if errors.As(err, &myErr) && myErr.Number == errDuplicateEntry {
			return bank.Transfer{}, bank.ErrIdempotencyKeyReused
		}
		return bank.Transfer{}, fmt.Errorf("insert transfer: %w", err)
	}
	if tr.ID, err = res.LastInsertId(); err != nil {
		return bank.Transfer{}, fmt.Errorf("transfer id: %w", err)
	}

	for _, u := range []struct {
		id      int64
		balance money.Sen
	}{{tr.FromAccountID, fromAfter}, {tr.ToAccountID, toAfter}} {
		if _, err := t.tx.ExecContext(ctx,
			`UPDATE accounts SET balance_sen = ? WHERE id = ?`, u.balance, u.id); err != nil {
			return bank.Transfer{}, fmt.Errorf("update balance: %w", err)
		}
	}

	_, err = t.tx.ExecContext(ctx, `
		INSERT INTO ledger_entries
			(transfer_id, account_id, counterparty_account_id, direction, amount_sen, balance_after_sen, reference, created_at)
		VALUES (?, ?, ?, 'DEBIT', ?, ?, ?, ?), (?, ?, ?, 'CREDIT', ?, ?, ?, ?)`,
		tr.ID, tr.FromAccountID, tr.ToAccountID, tr.Amount, fromAfter, tr.Reference, tr.CreatedAt,
		tr.ID, tr.ToAccountID, tr.FromAccountID, tr.Amount, toAfter, tr.Reference, tr.CreatedAt)
	if err != nil {
		return bank.Transfer{}, fmt.Errorf("insert ledger entries: %w", err)
	}
	return tr, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, so scanAccount
// works with either.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccount(row rowScanner) (bank.Account, error) {
	var a bank.Account
	err := row.Scan(&a.ID, &a.OwnerName, &a.Balance, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return bank.Account{}, bank.ErrAccountNotFound
	}
	if err != nil {
		return bank.Account{}, fmt.Errorf("scan account: %w", err)
	}
	return a, nil
}
