package bank_test

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/store/memstore"
)

// fakeClock lets a test decide what time it is.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// Wed 14 Oct 2026, 10:00 in Malaysia (02:00 UTC).
var defaultNow = time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)

type fixture struct {
	svc    *bank.Service
	clock  *fakeClock
	ctx    context.Context
	keySeq int
}

func newFixture(t *testing.T) *fixture {
	t.Helper() // failures are reported at the caller's line, not here
	clock := &fakeClock{now: defaultNow}
	return &fixture{
		svc:   bank.NewService(memstore.New(), bank.WithClock(clock.Now)),
		clock: clock,
		ctx:   context.Background(),
	}
}

func (f *fixture) account(t *testing.T, balance money.Sen) bank.Account {
	t.Helper()
	a, err := f.svc.CreateAccount(f.ctx, bank.CreateAccountRequest{OwnerName: "Test Owner", InitialBalance: balance})
	require.NoError(t, err)
	return a
}

func (f *fixture) balance(t *testing.T, id int64) money.Sen {
	t.Helper()
	a, err := f.svc.GetAccount(f.ctx, id)
	require.NoError(t, err)
	return a.Balance
}

// transfer sends money with a fresh idempotency key each time.
func (f *fixture) transfer(from, to int64, amount money.Sen) (bank.Transfer, error) {
	f.keySeq++
	return f.svc.Transfer(f.ctx, bank.TransferRequest{
		IdempotencyKey: fmt.Sprintf("test-key-%d", f.keySeq),
		FromAccountID:  from,
		ToAccountID:    to,
		Amount:         amount,
	})
}

func TestTransfer_MovesMoneyAndWritesLedger(t *testing.T) {
	f := newFixture(t)
	alice := f.account(t, money.FromRinggit(100))
	bob := f.account(t, money.FromRinggit(20))

	tr, err := f.svc.Transfer(f.ctx, bank.TransferRequest{
		IdempotencyKey: "k1", FromAccountID: alice.ID, ToAccountID: bob.ID, Amount: 25_50, Reference: "dinner",
	})

	require.NoError(t, err)
	assert.NotZero(t, tr.ID)
	assert.Equal(t, money.Sen(25_50), tr.Amount)
	assert.Equal(t, defaultNow, tr.CreatedAt)
	assert.Equal(t, money.Sen(74_50), f.balance(t, alice.ID))
	assert.Equal(t, money.Sen(45_50), f.balance(t, bob.ID))

	aliceHistory, err := f.svc.History(f.ctx, alice.ID, 0)
	require.NoError(t, err)
	require.Len(t, aliceHistory, 1)
	assert.Equal(t, bank.Debit, aliceHistory[0].Direction)
	assert.Equal(t, money.Sen(74_50), aliceHistory[0].BalanceAfter)
	assert.Equal(t, bob.ID, aliceHistory[0].CounterpartyAccountID)
	assert.Equal(t, "dinner", aliceHistory[0].Reference)

	bobHistory, err := f.svc.History(f.ctx, bob.ID, 0)
	require.NoError(t, err)
	require.Len(t, bobHistory, 1)
	assert.Equal(t, bank.Credit, bobHistory[0].Direction)
	assert.Equal(t, money.Sen(45_50), bobHistory[0].BalanceAfter)
}

func TestTransfer_Balance(t *testing.T) {
	tests := []struct {
		name    string
		balance money.Sen
		amount  money.Sen
		wantErr error
	}{
		{"amount below balance", 100_00, 99_99, nil},
		{"amount equals balance leaves zero", 100_00, 100_00, nil},
		{"amount exceeds balance by 1 sen", 100_00, 100_01, bank.ErrInsufficientFunds},
		{"empty account", 0, 1, bank.ErrInsufficientFunds},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			from := f.account(t, tc.balance)
			to := f.account(t, 0)

			_, err := f.transfer(from.ID, to.ID, tc.amount)

			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				assert.Equal(t, tc.balance, f.balance(t, from.ID), "failed transfer must not change sender balance")
				assert.Equal(t, money.Sen(0), f.balance(t, to.ID), "failed transfer must not change receiver balance")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.balance-tc.amount, f.balance(t, from.ID))
			assert.Equal(t, tc.amount, f.balance(t, to.ID))
		})
	}
}

func TestTransfer_CreditThatWouldOverflowReceiverIsRejected(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(100))
	to := f.account(t, money.Sen(math.MaxInt64))

	_, err := f.transfer(from.ID, to.ID, 1)

	assert.ErrorIs(t, err, bank.ErrBalanceLimit)
	assert.Equal(t, money.Sen(math.MaxInt64), f.balance(t, to.ID))
	assert.Equal(t, money.FromRinggit(100), f.balance(t, from.ID))
}

func TestTransfer_RejectsInvalidRequestBeforeTouchingStorage(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(100))
	to := f.account(t, 0)

	_, err := f.transfer(from.ID, to.ID, 0)

	assert.ErrorIs(t, err, bank.ErrInvalidAmount)
	assert.Equal(t, money.FromRinggit(100), f.balance(t, from.ID))
}

func TestTransfer_AccountNotFound(t *testing.T) {
	f := newFixture(t)
	existing := f.account(t, money.FromRinggit(100))

	_, err := f.transfer(existing.ID, 999, 1_00)
	assert.ErrorIs(t, err, bank.ErrAccountNotFound, "unknown receiver")

	_, err = f.transfer(999, existing.ID, 1_00)
	assert.ErrorIs(t, err, bank.ErrAccountNotFound, "unknown sender")

	assert.Equal(t, money.FromRinggit(100), f.balance(t, existing.ID))
}

func TestTransfer_DailyLimit(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(20_000))
	to := f.account(t, 0)

	_, err := f.transfer(from.ID, to.ID, money.FromRinggit(5_000))
	require.NoError(t, err)
	_, err = f.transfer(from.ID, to.ID, money.FromRinggit(5_000))
	require.NoError(t, err, "exactly RM 10,000 in a day is allowed")

	_, err = f.transfer(from.ID, to.ID, 1)
	assert.ErrorIs(t, err, bank.ErrDailyLimit, "1 sen over the daily limit")
	assert.Equal(t, money.FromRinggit(10_000), f.balance(t, from.ID), "rejected transfer must not debit")
}

func TestTransfer_DailyLimitErrorReportsRemainingAllowance(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(20_000))
	to := f.account(t, 0)
	_, err := f.transfer(from.ID, to.ID, money.FromRinggit(4_000))
	require.NoError(t, err)
	_, err = f.transfer(from.ID, to.ID, money.FromRinggit(4_000))
	require.NoError(t, err)

	_, err = f.transfer(from.ID, to.ID, money.FromRinggit(2_500))

	require.ErrorIs(t, err, bank.ErrDailyLimit)
	assert.Contains(t, err.Error(), "remaining today: RM 2,000.00")
}

func TestTransfer_IncomingCreditsDoNotCountTowardDailyLimit(t *testing.T) {
	f := newFixture(t)
	a := f.account(t, money.FromRinggit(20_000))
	b := f.account(t, money.FromRinggit(20_000))

	// b receives RM 10,000 ...
	_, err := f.transfer(a.ID, b.ID, money.FromRinggit(5_000))
	require.NoError(t, err)
	_, err = f.transfer(a.ID, b.ID, money.FromRinggit(5_000))
	require.NoError(t, err)

	// ... and can still send its own full daily allowance.
	_, err = f.transfer(b.ID, a.ID, money.FromRinggit(5_000))
	assert.NoError(t, err)
}

// The daily limit resets at midnight Malaysia time (UTC+8), which is
// 16:00 UTC. A UTC-based implementation would reset 8 hours late.
func TestTransfer_DailyLimitResetsAtMidnightMalaysiaTime(t *testing.T) {
	tests := []struct {
		name         string
		spentAt      time.Time
		retryAt      time.Time
		wantRejected bool
	}{
		{
			name:         "same Malaysian day, one second before midnight",
			spentAt:      time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC),    // 09:00 MYT 14 Oct
			retryAt:      time.Date(2026, 10, 14, 15, 59, 59, 0, time.UTC), // 23:59:59 MYT 14 Oct
			wantRejected: true,
		},
		{
			name:         "next Malaysian day at exactly midnight",
			spentAt:      time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC),  // 09:00 MYT 14 Oct
			retryAt:      time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC), // 00:00 MYT 15 Oct
			wantRejected: false,
		},
		{
			name:         "crossing UTC midnight is still the same Malaysian day",
			spentAt:      time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC), // 07:00 MYT 14 Oct
			retryAt:      time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC),  // 09:00 MYT 14 Oct
			wantRejected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			from := f.account(t, money.FromRinggit(20_000))
			to := f.account(t, 0)

			f.clock.now = tc.spentAt
			for range 2 {
				_, err := f.transfer(from.ID, to.ID, money.FromRinggit(5_000))
				require.NoError(t, err)
			}

			f.clock.now = tc.retryAt
			_, err := f.transfer(from.ID, to.ID, money.FromRinggit(1))

			if tc.wantRejected {
				assert.ErrorIs(t, err, bank.ErrDailyLimit)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTransfer_SameIdempotencyKeyReturnsOriginalWithoutDebitingTwice(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(100))
	to := f.account(t, 0)
	req := bank.TransferRequest{IdempotencyKey: "retry-me", FromAccountID: from.ID, ToAccountID: to.ID, Amount: money.FromRinggit(30)}

	first, err := f.svc.Transfer(f.ctx, req)
	require.NoError(t, err)
	second, err := f.svc.Transfer(f.ctx, req)
	require.NoError(t, err)

	assert.Equal(t, first, second, "retry must return the original transfer")
	assert.Equal(t, money.FromRinggit(70), f.balance(t, from.ID), "sender debited exactly once")
	assert.Equal(t, money.FromRinggit(30), f.balance(t, to.ID), "receiver credited exactly once")
}

// If the first attempt spent the whole balance, a retry must still replay
// the success rather than fail with "insufficient balance".
func TestTransfer_RetryAfterBalanceSpentReplaysOriginalSuccess(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(50))
	to := f.account(t, 0)
	req := bank.TransferRequest{IdempotencyKey: "drain", FromAccountID: from.ID, ToAccountID: to.ID, Amount: money.FromRinggit(50)}

	first, err := f.svc.Transfer(f.ctx, req)
	require.NoError(t, err)
	second, err := f.svc.Transfer(f.ctx, req)

	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, money.Sen(0), f.balance(t, from.ID))
}

func TestTransfer_IdempotencyKeyReusedForDifferentRequest(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(100))
	to := f.account(t, 0)
	req := bank.TransferRequest{IdempotencyKey: "shared", FromAccountID: from.ID, ToAccountID: to.ID, Amount: money.FromRinggit(10)}
	_, err := f.svc.Transfer(f.ctx, req)
	require.NoError(t, err)

	req.Amount = money.FromRinggit(20)
	_, err = f.svc.Transfer(f.ctx, req)

	assert.ErrorIs(t, err, bank.ErrIdempotencyKeyReused)
	assert.Equal(t, money.FromRinggit(90), f.balance(t, from.ID))
}

// Fire many transfers at once from one account. Without locking, several
// goroutines would read the same balance and all succeed, overdrawing it.
// Run with -race to also catch unsynchronised memory access.
func TestTransfer_ConcurrentTransfersNeverOverdraw(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(100))
	to := f.account(t, 0)

	const workers = 50
	amount := money.FromRinggit(10) // only 10 of the 50 can succeed

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		rejected  int
	)
	// A WaitGroup counts running goroutines; wg.Wait blocks until all
	// have called Done.
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.Transfer(f.ctx, bank.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromAccountID:  from.ID,
				ToAccountID:    to.ID,
				Amount:         amount,
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case assert.ErrorIs(t, err, bank.ErrInsufficientFunds):
				rejected++
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 10, succeeded)
	assert.Equal(t, workers-10, rejected)
	assert.Equal(t, money.Sen(0), f.balance(t, from.ID), "balance must never go negative")
	assert.Equal(t, money.FromRinggit(100), f.balance(t, to.ID), "money is neither created nor lost")
}

// The just-before-midnight case: 23:59:59.9999996 MYT must count towards
// that day, not be rounded into the next one when stored.
func TestTransfer_TimestampIsTruncatedToMicroseconds(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(100))
	to := f.account(t, 0)
	f.clock.now = time.Date(2026, 10, 14, 15, 59, 59, 999_999_600, time.UTC)

	tr, err := f.transfer(from.ID, to.ID, 1_00)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 14, 15, 59, 59, 999_999_000, time.UTC), tr.CreatedAt)
}

func TestCreateAccount_TrimsOwnerName(t *testing.T) {
	f := newFixture(t)

	a, err := f.svc.CreateAccount(f.ctx, bank.CreateAccountRequest{OwnerName: "  Nurul  ", InitialBalance: 1})

	require.NoError(t, err)
	assert.Equal(t, "Nurul", a.OwnerName)
	assert.Equal(t, defaultNow, a.CreatedAt)
}

func TestGetAccount(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.GetAccount(f.ctx, 0)
	assert.ErrorIs(t, err, bank.ErrInvalidAccountID)

	_, err = f.svc.GetAccount(f.ctx, 42)
	assert.ErrorIs(t, err, bank.ErrAccountNotFound)
}

func TestHistory_NewestFirstAndLimited(t *testing.T) {
	f := newFixture(t)
	from := f.account(t, money.FromRinggit(1_000))
	to := f.account(t, 0)
	for i := 1; i <= 5; i++ {
		_, err := f.transfer(from.ID, to.ID, money.Sen(i))
		require.NoError(t, err)
	}

	entries, err := f.svc.History(f.ctx, from.ID, 3)

	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, []money.Sen{5, 4, 3}, []money.Sen{entries[0].Amount, entries[1].Amount, entries[2].Amount})
}

func TestHistory_UnknownAccount(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.History(f.ctx, 999, 0)

	assert.ErrorIs(t, err, bank.ErrAccountNotFound)
}
