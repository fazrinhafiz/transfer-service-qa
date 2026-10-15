package bank

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

const (
	maxOwnerNameLen      = 100
	maxReferenceLen      = 140
	maxIdempotencyKeyLen = 64
)

// Limits are the transfer limits enforced by the service.
type Limits struct {
	PerTransaction money.Sen
	Daily          money.Sen
}

// DefaultLimits are RM 5,000 per transfer and RM 10,000 per day.
var DefaultLimits = Limits{
	PerTransaction: money.FromRinggit(5_000),
	Daily:          money.FromRinggit(10_000),
}

func (r CreateAccountRequest) Validate() error {
	name := strings.TrimSpace(r.OwnerName)
	if name == "" || utf8.RuneCountInString(name) > maxOwnerNameLen {
		return ErrInvalidOwnerName
	}
	if r.InitialBalance < 0 {
		return ErrInvalidInitialBalance
	}
	return nil
}

// Validate checks everything that can be decided without reading storage.
// Rules that depend on stored state (balance, daily total) are checked by
// the service while the accounts are locked.
func (r TransferRequest) Validate(limits Limits) error {
	switch {
	case r.IdempotencyKey == "":
		return ErrMissingIdempotencyKey
	case len(r.IdempotencyKey) > maxIdempotencyKeyLen:
		return ErrInvalidIdempotencyKey
	case r.FromAccountID <= 0 || r.ToAccountID <= 0:
		return ErrInvalidAccountID
	case r.FromAccountID == r.ToAccountID:
		return ErrSameAccount
	case r.Amount <= 0:
		return ErrInvalidAmount
	case r.Amount > limits.PerTransaction:
		return fmt.Errorf("%w of %s", ErrPerTransactionLimit, limits.PerTransaction)
	case utf8.RuneCountInString(r.Reference) > maxReferenceLen:
		return ErrReferenceTooLong
	}
	return nil
}
