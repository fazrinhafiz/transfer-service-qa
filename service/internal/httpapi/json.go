package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// errInvalidJSON and errNotWholeSen are request-decoding errors that only
// exist at the HTTP layer.
var (
	errInvalidJSON  = errors.New("request body must be a single valid JSON object with known fields")
	errNotWholeSen  = errors.New("amounts must be a whole number of sen, e.g. 1050 for RM 10.50")
	errInvalidLimit = errors.New("limit must be an integer between 1 and 100")
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorMappings translates domain errors into HTTP status codes and stable,
// machine-readable error codes. Tests assert on the code, not the message.
var errorMappings = []struct {
	err    error
	status int
	code   string
}{
	{errInvalidJSON, http.StatusBadRequest, "INVALID_JSON"},
	{errNotWholeSen, http.StatusBadRequest, "INVALID_AMOUNT"},
	{errInvalidLimit, http.StatusBadRequest, "INVALID_LIMIT"},
	{bank.ErrInvalidOwnerName, http.StatusBadRequest, "INVALID_OWNER_NAME"},
	{bank.ErrInvalidInitialBalance, http.StatusBadRequest, "INVALID_AMOUNT"},
	{bank.ErrInvalidAccountID, http.StatusBadRequest, "INVALID_ACCOUNT_ID"},
	{bank.ErrInvalidAmount, http.StatusBadRequest, "INVALID_AMOUNT"},
	{bank.ErrSameAccount, http.StatusBadRequest, "SAME_ACCOUNT"},
	{bank.ErrReferenceTooLong, http.StatusBadRequest, "INVALID_REFERENCE"},
	{bank.ErrMissingIdempotencyKey, http.StatusBadRequest, "MISSING_IDEMPOTENCY_KEY"},
	{bank.ErrInvalidIdempotencyKey, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY"},
	{bank.ErrAccountNotFound, http.StatusNotFound, "ACCOUNT_NOT_FOUND"},
	{bank.ErrPerTransactionLimit, http.StatusUnprocessableEntity, "PER_TRANSACTION_LIMIT_EXCEEDED"},
	{bank.ErrDailyLimit, http.StatusUnprocessableEntity, "DAILY_LIMIT_EXCEEDED"},
	{bank.ErrInsufficientFunds, http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS"},
	{bank.ErrBalanceLimit, http.StatusUnprocessableEntity, "BALANCE_LIMIT_EXCEEDED"},
	{bank.ErrIdempotencyKeyReused, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED"},
}

// errorResponse returns the status and body for err. Unknown errors become
// a generic 500 so internal details never leak to clients.
func errorResponse(err error) (int, errorBody) {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			return m.status, errorBody{errorDetail{Code: m.code, Message: err.Error()}}
		}
	}
	return http.StatusInternalServerError, errorBody{errorDetail{Code: "INTERNAL_ERROR", Message: "internal server error"}}
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := errorResponse(err)
	if status == http.StatusInternalServerError {
		h.log.Error("request failed", "path", r.URL.Path, "error", err)
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`, http.StatusInternalServerError)
		return
	}
	writeRaw(w, status, body)
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// decodeJSON strictly decodes the request body into dst: unknown fields,
// trailing data and oversized bodies are rejected.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		// A decimal (10.5), a string ("100") or an out-of-range number in an
		// amount field fails to decode into int64. Report it as an amount
		// problem rather than generic bad JSON, so clients know what to fix.
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && isAmountField(typeErr.Field) {
			return errNotWholeSen
		}
		return errInvalidJSON
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errInvalidJSON
	}
	return nil
}

func isAmountField(field string) bool {
	return field == "amount_sen" || field == "initial_balance_sen"
}
