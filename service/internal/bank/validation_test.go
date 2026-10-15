// Tests in package bank_test (note the _test suffix) can only use the
// exported API, exactly like a real caller would.
package bank_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
)

func validTransfer() bank.TransferRequest {
	return bank.TransferRequest{
		IdempotencyKey: "key-1",
		FromAccountID:  1,
		ToAccountID:    2,
		Amount:         10_00,
		Reference:      "lunch",
	}
}

// Table-driven test: each row is one case, and t.Run gives every case its
// own name in the output, e.g. TestTransferRequestValidate/zero_amount.
func TestTransferRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(r *bank.TransferRequest)
		wantErr error
	}{
		{"valid request", func(r *bank.TransferRequest) {}, nil},
		{"smallest amount: 1 sen", func(r *bank.TransferRequest) { r.Amount = 1 }, nil},
		{"exactly the per-transaction limit", func(r *bank.TransferRequest) { r.Amount = 5_000_00 }, nil},
		{"1 sen over the per-transaction limit", func(r *bank.TransferRequest) { r.Amount = 5_000_01 }, bank.ErrPerTransactionLimit},
		{"zero amount", func(r *bank.TransferRequest) { r.Amount = 0 }, bank.ErrInvalidAmount},
		{"negative amount", func(r *bank.TransferRequest) { r.Amount = -100 }, bank.ErrInvalidAmount},
		{"same account", func(r *bank.TransferRequest) { r.ToAccountID = r.FromAccountID }, bank.ErrSameAccount},
		{"missing from account", func(r *bank.TransferRequest) { r.FromAccountID = 0 }, bank.ErrInvalidAccountID},
		{"negative to account", func(r *bank.TransferRequest) { r.ToAccountID = -1 }, bank.ErrInvalidAccountID},
		{"missing idempotency key", func(r *bank.TransferRequest) { r.IdempotencyKey = "" }, bank.ErrMissingIdempotencyKey},
		{"idempotency key at max length", func(r *bank.TransferRequest) { r.IdempotencyKey = strings.Repeat("k", 64) }, nil},
		{"idempotency key too long", func(r *bank.TransferRequest) { r.IdempotencyKey = strings.Repeat("k", 65) }, bank.ErrInvalidIdempotencyKey},
		{"reference at max length", func(r *bank.TransferRequest) { r.Reference = strings.Repeat("r", 140) }, nil},
		{"reference too long", func(r *bank.TransferRequest) { r.Reference = strings.Repeat("r", 141) }, bank.ErrReferenceTooLong},
		{"reference length counts characters, not bytes", func(r *bank.TransferRequest) { r.Reference = strings.Repeat("é", 140) }, nil},
		{"empty reference is allowed", func(r *bank.TransferRequest) { r.Reference = "" }, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validTransfer()
			tc.modify(&req)

			err := req.Validate(bank.DefaultLimits)

			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestCreateAccountRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     bank.CreateAccountRequest
		wantErr error
	}{
		{"valid", bank.CreateAccountRequest{OwnerName: "Aisyah", InitialBalance: 100_00}, nil},
		{"zero balance is allowed", bank.CreateAccountRequest{OwnerName: "Aisyah", InitialBalance: 0}, nil},
		{"negative balance", bank.CreateAccountRequest{OwnerName: "Aisyah", InitialBalance: -1}, bank.ErrInvalidInitialBalance},
		{"empty name", bank.CreateAccountRequest{OwnerName: "", InitialBalance: 0}, bank.ErrInvalidOwnerName},
		{"whitespace-only name", bank.CreateAccountRequest{OwnerName: "   ", InitialBalance: 0}, bank.ErrInvalidOwnerName},
		{"name at max length", bank.CreateAccountRequest{OwnerName: strings.Repeat("a", 100)}, nil},
		{"name too long", bank.CreateAccountRequest{OwnerName: strings.Repeat("a", 101)}, bank.ErrInvalidOwnerName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}
