package bank

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

// malaysiaTime is UTC+8. Malaysia has no daylight saving, so a fixed zone
// is exact and avoids depending on the tz database inside the container.
var malaysiaTime = time.FixedZone("MYT", 8*60*60)

const (
	defaultHistoryLimit = 20
	maxHistoryLimit     = 100
)

type Service struct {
	store  Store
	limits Limits
	now    func() time.Time
}

// Option configures a Service. This "functional options" pattern keeps the
// constructor simple for production while letting tests override defaults.
type Option func(*Service)

// WithClock replaces time.Now, so tests can control which day it is.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

func WithLimits(l Limits) Option {
	return func(s *Service) { s.limits = l }
}

func NewService(store Store, opts ...Option) *Service {
	s := &Service{store: store, limits: DefaultLimits, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) Limits() Limits { return s.limits }

func (s *Service) CreateAccount(ctx context.Context, req CreateAccountRequest) (Account, error) {
	if err := req.Validate(); err != nil {
		return Account{}, err
	}
	return s.store.CreateAccount(ctx, Account{
		OwnerName: strings.TrimSpace(req.OwnerName),
		Balance:   req.InitialBalance,
		CreatedAt: s.timestamp(),
	})
}

func (s *Service) GetAccount(ctx context.Context, id int64) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidAccountID
	}
	return s.store.GetAccount(ctx, id)
}

// History returns the most recent ledger entries for an account, newest first.
// A limit of 0 means the default; larger values are capped.
func (s *Service) History(ctx context.Context, accountID int64, limit int) ([]LedgerEntry, error) {
	if _, err := s.GetAccount(ctx, accountID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	limit = min(limit, maxHistoryLimit)
	return s.store.ListLedger(ctx, accountID, limit)
}

// Transfer moves money between two accounts.
//
// All checks that depend on stored state run inside InTransferTx, while
// both account rows are locked. That is what makes concurrent transfers
// safe: two requests from the same account are forced to run one after
// the other, so each sees the balance the previous one left behind.
func (s *Service) Transfer(ctx context.Context, req TransferRequest) (Transfer, error) {
	if err := req.Validate(s.limits); err != nil {
		return Transfer{}, err
	}

	now := s.timestamp()
	var result Transfer

	err := s.store.InTransferTx(ctx, req.FromAccountID, req.ToAccountID, func(tx TransferTx, from, to Account) error {
		// Database-level idempotency. Redis normally answers retries before
		// they get here; this covers the case where the Redis key has expired
		// or been lost, so a retry can still never debit twice.
		existing, found, err := tx.FindTransfer(ctx, req.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if !existing.matches(req) {
				return ErrIdempotencyKeyReused
			}
			result = existing
			return nil
		}

		if from.Balance < req.Amount {
			return ErrInsufficientFunds
		}
		// Go integers wrap around silently on overflow: MaxInt64 + 1 becomes
		// a large negative number. Check before adding, not after.
		if to.Balance > money.Sen(math.MaxInt64)-req.Amount {
			return ErrBalanceLimit
		}

		sentToday, err := tx.SumDebitsSince(ctx, from.ID, startOfDay(now))
		if err != nil {
			return err
		}
		if sentToday+req.Amount > s.limits.Daily {
			remaining := max(s.limits.Daily-sentToday, 0)
			return fmt.Errorf("%w of %s (remaining today: %s)", ErrDailyLimit, s.limits.Daily, remaining)
		}

		result, err = tx.SaveTransfer(ctx, Transfer{
			IdempotencyKey: req.IdempotencyKey,
			FromAccountID:  from.ID,
			ToAccountID:    to.ID,
			Amount:         req.Amount,
			Reference:      req.Reference,
			CreatedAt:      now,
		}, from.Balance-req.Amount, to.Balance+req.Amount)
		return err
	})
	if err != nil {
		return Transfer{}, err
	}
	return result, nil
}

// timestamp returns the current UTC time truncated to microseconds, the
// precision of MySQL DATETIME(6). MySQL rounds extra digits rather than
// dropping them, so without this the API would return a different time from
// the one stored, and 23:59:59.9999996 could round into the next day and be
// counted against the wrong daily limit.
func (s *Service) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

// startOfDay returns midnight Malaysia time for the day containing t, in
// UTC. The daily limit resets at 00:00 MYT, which is 16:00 UTC.
func startOfDay(t time.Time) time.Time {
	local := t.In(malaysiaTime)
	y, m, d := local.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, malaysiaTime).UTC()
}
