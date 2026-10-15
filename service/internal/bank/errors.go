package bank

import "errors"

// Sentinel errors. Callers compare with errors.Is, which still matches when
// an error has been wrapped with extra detail via fmt.Errorf("%w: ...").
var (
	// Validation: the request itself is malformed.
	ErrInvalidOwnerName      = errors.New("owner_name must be 1-100 characters")
	ErrInvalidInitialBalance = errors.New("initial_balance_sen must not be negative")
	ErrInvalidAccountID      = errors.New("account id must be a positive integer")
	ErrInvalidAmount         = errors.New("amount_sen must be greater than zero")
	ErrSameAccount           = errors.New("cannot transfer to the same account")
	ErrReferenceTooLong      = errors.New("reference must be at most 140 characters")
	ErrMissingIdempotencyKey = errors.New("Idempotency-Key header is required")
	ErrInvalidIdempotencyKey = errors.New("Idempotency-Key must be at most 64 characters")
	ErrPerTransactionLimit   = errors.New("amount exceeds the per-transaction limit")

	// Business rules: the request is well formed but cannot be honoured.
	ErrAccountNotFound      = errors.New("account not found")
	ErrInsufficientFunds    = errors.New("insufficient balance")
	ErrDailyLimit           = errors.New("transfer would exceed the daily limit")
	ErrBalanceLimit         = errors.New("transfer would exceed the receiver's maximum balance")
	ErrIdempotencyKeyReused = errors.New("Idempotency-Key was already used for a different request")
)
