package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/idempotency"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

const (
	headerIdempotencyKey = "Idempotency-Key"
	headerReplayed       = "Idempotent-Replayed"
)

type createTransferRequest struct {
	FromAccountID int64  `json:"from_account_id"`
	ToAccountID   int64  `json:"to_account_id"`
	AmountSen     int64  `json:"amount_sen"`
	Reference     string `json:"reference"`
}

type transferResponse struct {
	ID            int64     `json:"id"`
	FromAccountID int64     `json:"from_account_id"`
	ToAccountID   int64     `json:"to_account_id"`
	AmountSen     int64     `json:"amount_sen"`
	Amount        string    `json:"amount"`
	Reference     string    `json:"reference"`
	CreatedAt     time.Time `json:"created_at"`
}

// createTransfer handles POST /transfers:
//
//  1. Decode and validate the request. Invalid requests are rejected before
//     the idempotency key is touched, so fixing a typo and retrying works.
//  2. Reserve the key. If it already exists, replay the stored response
//     (or report a conflict) without executing the transfer again.
//  3. Execute the transfer and store its response against the key.
func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}
	req := bank.TransferRequest{
		IdempotencyKey: r.Header.Get(headerIdempotencyKey),
		FromAccountID:  body.FromAccountID,
		ToAccountID:    body.ToAccountID,
		Amount:         money.Sen(body.AmountSen),
		Reference:      body.Reference,
	}
	if err := req.Validate(h.svc.Limits()); err != nil {
		h.writeError(w, r, err)
		return
	}

	fp := fingerprint(req)
	existing, reserved, err := h.idem.Begin(r.Context(), req.IdempotencyKey, fp)
	if err != nil {
		h.writeError(w, r, fmt.Errorf("idempotency begin: %w", err))
		return
	}
	if !reserved {
		h.replay(w, r, existing, fp)
		return
	}

	status, respBody := h.executeTransfer(r, req)

	// Record the outcome even if the client has disconnected:
	// context.WithoutCancel keeps the request's values but ignores its
	// cancellation, so the bookkeeping below always runs.
	ctx := context.WithoutCancel(r.Context())
	if status >= http.StatusInternalServerError {
		// Unexpected failure: nothing was committed, so free the key and
		// let the client retry with the same key.
		if err := h.idem.Release(ctx, req.IdempotencyKey); err != nil {
			h.log.Error("release idempotency key", "error", err)
		}
	} else {
		rec := idempotency.Record{Fingerprint: fp, StatusCode: status, Body: respBody}
		if err := h.idem.Complete(ctx, req.IdempotencyKey, rec); err != nil {
			h.log.Error("complete idempotency key", "error", err)
		}
	}
	writeRaw(w, status, respBody)
}

func (h *Handler) executeTransfer(r *http.Request, req bank.TransferRequest) (int, []byte) {
	var (
		status  int
		payload any
	)
	t, err := h.svc.Transfer(r.Context(), req)
	if err != nil {
		var body errorBody
		status, body = errorResponse(err)
		if status == http.StatusInternalServerError {
			h.log.Error("transfer failed", "error", err)
		}
		payload = body
	} else {
		status, payload = http.StatusCreated, transferResponse{
			ID:            t.ID,
			FromAccountID: t.FromAccountID,
			ToAccountID:   t.ToAccountID,
			AmountSen:     int64(t.Amount),
			Amount:        t.Amount.String(),
			Reference:     t.Reference,
			CreatedAt:     t.CreatedAt,
		}
	}

	respBody, err := json.Marshal(payload)
	if err != nil {
		return http.StatusInternalServerError, []byte(`{"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`)
	}
	return status, respBody
}

func (h *Handler) replay(w http.ResponseWriter, r *http.Request, rec idempotency.Record, fp string) {
	switch {
	case rec.Fingerprint != fp:
		h.writeError(w, r, bank.ErrIdempotencyKeyReused)
	case !rec.Completed:
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusConflict, errorBody{errorDetail{
			Code:    "REQUEST_IN_PROGRESS",
			Message: "a request with this Idempotency-Key is still being processed",
		}})
	default:
		w.Header().Set(headerReplayed, "true")
		writeRaw(w, rec.StatusCode, rec.Body)
	}
}

// fingerprint identifies a transfer request by its meaning rather than its
// raw bytes, so whitespace or field order in the JSON doesn't matter.
func fingerprint(req bank.TransferRequest) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d|%d|%d|%s",
		req.FromAccountID, req.ToAccountID, req.Amount, req.Reference))
	return hex.EncodeToString(sum[:])
}
