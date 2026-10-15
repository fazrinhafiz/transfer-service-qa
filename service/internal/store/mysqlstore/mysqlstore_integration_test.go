//go:build integration

// Integration tests against a real MySQL. The build tag above excludes this
// file from a plain `go test ./...`; run it with:
//
//	MYSQL_DSN='bank:bank@tcp(localhost:3306)/bank?parseTime=true&loc=UTC' \
//	  go test -tags integration ./internal/store/mysqlstore/
//
// These prove what the in-memory store cannot: that InnoDB row locks
// serialise concurrent transfers and that opposite transfers don't deadlock.
package mysqlstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/store/mysqlstore"
)

func newService(t *testing.T) (*bank.Service, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		t.Skip("MYSQL_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(50)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Ping())
	return bank.NewService(mysqlstore.New(db)), db
}

// uniqueKey avoids collisions with earlier runs against the same database.
func uniqueKey(suffix string) string {
	return fmt.Sprintf("it-%d-%s", time.Now().UnixNano(), suffix)
}

func createAccount(t *testing.T, svc *bank.Service, balance money.Sen) bank.Account {
	t.Helper()
	a, err := svc.CreateAccount(context.Background(), bank.CreateAccountRequest{OwnerName: "Integration", InitialBalance: balance})
	require.NoError(t, err)
	return a
}

func balanceOf(t *testing.T, svc *bank.Service, id int64) money.Sen {
	t.Helper()
	a, err := svc.GetAccount(context.Background(), id)
	require.NoError(t, err)
	return a.Balance
}

func TestMySQL_TransferRoundTrip(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	from := createAccount(t, svc, money.FromRinggit(100))
	to := createAccount(t, svc, 0)

	tr, err := svc.Transfer(ctx, bank.TransferRequest{
		IdempotencyKey: uniqueKey("rt"), FromAccountID: from.ID, ToAccountID: to.ID, Amount: 12_34, Reference: "round trip",
	})
	require.NoError(t, err)

	assert.Equal(t, money.Sen(100_00-12_34), balanceOf(t, svc, from.ID))
	assert.Equal(t, money.Sen(12_34), balanceOf(t, svc, to.ID))

	history, err := svc.History(ctx, to.ID, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, tr.ID, history[0].TransferID)
	assert.Equal(t, bank.Credit, history[0].Direction)
	assert.Equal(t, "round trip", history[0].Reference)
	assert.True(t, tr.CreatedAt.Equal(history[0].CreatedAt),
		"returned timestamp %s must equal stored timestamp %s", tr.CreatedAt, history[0].CreatedAt)
}

func TestMySQL_ConcurrentTransfersNeverOverdraw(t *testing.T) {
	svc, _ := newService(t)
	from := createAccount(t, svc, money.FromRinggit(100))
	to := createAccount(t, svc, 0)

	const workers = 40
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Transfer(context.Background(), bank.TransferRequest{
				IdempotencyKey: uniqueKey(fmt.Sprintf("c%d", i)),
				FromAccountID:  from.ID, ToAccountID: to.ID, Amount: money.FromRinggit(10),
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	succeeded := 0
	for err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		assert.ErrorIs(t, err, bank.ErrInsufficientFunds)
	}
	assert.Equal(t, 10, succeeded)
	assert.Equal(t, money.Sen(0), balanceOf(t, svc, from.ID))
	assert.Equal(t, money.FromRinggit(100), balanceOf(t, svc, to.ID))
}

// A->B and B->A at the same time is the classic deadlock. Because accounts
// are always locked lowest-id first, every transfer must complete.
func TestMySQL_OppositeTransfersDoNotDeadlock(t *testing.T) {
	svc, _ := newService(t)
	a := createAccount(t, svc, money.FromRinggit(1_000))
	b := createAccount(t, svc, money.FromRinggit(1_000))

	const perDirection = 25
	var wg sync.WaitGroup
	errs := make(chan error, 2*perDirection)
	for i := range perDirection {
		for _, pair := range [][2]int64{{a.ID, b.ID}, {b.ID, a.ID}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.Transfer(context.Background(), bank.TransferRequest{
					IdempotencyKey: uniqueKey(fmt.Sprintf("d%d-%d", pair[0], i)),
					FromAccountID:  pair[0], ToAccountID: pair[1], Amount: 1_00,
				})
				errs <- err
			}()
		}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}
	assert.Equal(t, money.FromRinggit(2_000), balanceOf(t, svc, a.ID)+balanceOf(t, svc, b.ID), "total money is conserved")
}

func TestMySQL_IdempotencyBackstopWithoutRedis(t *testing.T) {
	svc, _ := newService(t)
	from := createAccount(t, svc, money.FromRinggit(100))
	to := createAccount(t, svc, 0)
	req := bank.TransferRequest{IdempotencyKey: uniqueKey("dup"), FromAccountID: from.ID, ToAccountID: to.ID, Amount: money.FromRinggit(5)}

	first, err := svc.Transfer(context.Background(), req)
	require.NoError(t, err)
	second, err := svc.Transfer(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, money.FromRinggit(95), balanceOf(t, svc, from.ID))
}

// Defence in depth: even a buggy UPDATE cannot store a negative balance.
func TestMySQL_CheckConstraintRejectsNegativeBalance(t *testing.T) {
	svc, db := newService(t)
	acc := createAccount(t, svc, 0)

	_, err := db.Exec(`UPDATE accounts SET balance_sen = -1 WHERE id = ?`, acc.ID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chk_accounts_balance_non_negative")
}
