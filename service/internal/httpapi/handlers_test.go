// These tests are in package httpapi (not httpapi_test) so they can use the
// unexported fingerprint function to simulate an in-flight request.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/idempotency"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/store/memstore"
)

type testServer struct {
	t       *testing.T
	handler http.Handler
	idem    *idempotency.MemoryStore
}

func newTestServer(t *testing.T) *testServer {
	return newTestServerWithStore(t, memstore.New())
}

func newTestServerWithStore(t *testing.T, store bank.Store, checks ...HealthCheck) *testServer {
	t.Helper()
	idem := idempotency.NewMemoryStore()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &testServer{
		t:       t,
		handler: NewRouter(bank.NewService(store), idem, log, checks...),
		idem:    idem,
	}
}

// do sends a request straight to the router: httptest.NewRecorder captures
// the response in memory, so no network port is opened.
func (s *testServer) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func (s *testServer) createAccount(balanceSen int64) int64 {
	s.t.Helper()
	rec := s.do(http.MethodPost, "/accounts", fmt.Sprintf(`{"owner_name":"Test","initial_balance_sen":%d}`, balanceSen), nil)
	require.Equal(s.t, http.StatusCreated, rec.Code, rec.Body.String())
	return decode[accountResponse](s.t, rec).ID
}

func (s *testServer) balance(id int64) int64 {
	s.t.Helper()
	rec := s.do(http.MethodGet, fmt.Sprintf("/accounts/%d", id), "", nil)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return decode[accountResponse](s.t, rec).BalanceSen
}

func (s *testServer) transfer(key, body string) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.do(http.MethodPost, "/transfers", body, map[string]string{"Idempotency-Key": key})
}

// decode is a generic function: T is chosen by the caller, e.g.
// decode[accountResponse](t, rec), and the JSON is parsed into a T.
func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[errorBody](t, rec).Error.Code
}

func transferBody(from, to, amount int64) string {
	return fmt.Sprintf(`{"from_account_id":%d,"to_account_id":%d,"amount_sen":%d,"reference":"test"}`, from, to, amount)
}

// --- Accounts ---

func TestCreateAccount_Success(t *testing.T) {
	s := newTestServer(t)

	rec := s.do(http.MethodPost, "/accounts", `{"owner_name":"Aisyah","initial_balance_sen":123456}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	acc := decode[accountResponse](t, rec)
	assert.Equal(t, fmt.Sprintf("/accounts/%d", acc.ID), rec.Header().Get("Location"))
	assert.Equal(t, "Aisyah", acc.OwnerName)
	assert.Equal(t, int64(123456), acc.BalanceSen)
	assert.Equal(t, "RM 1,234.56", acc.Balance)
}

func TestCreateAccount_Rejects(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"malformed JSON", `{"owner_name":`, "INVALID_JSON"},
		{"unknown field", `{"owner_name":"A","initial_balance_sen":0,"is_admin":true}`, "INVALID_JSON"},
		{"two JSON objects", `{"owner_name":"A"}{"owner_name":"B"}`, "INVALID_JSON"},
		{"decimal balance", `{"owner_name":"A","initial_balance_sen":10.5}`, "INVALID_AMOUNT"},
		{"balance as string", `{"owner_name":"A","initial_balance_sen":"100"}`, "INVALID_AMOUNT"},
		{"negative balance", `{"owner_name":"A","initial_balance_sen":-1}`, "INVALID_AMOUNT"},
		{"missing owner name", `{"initial_balance_sen":100}`, "INVALID_OWNER_NAME"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)

			rec := s.do(http.MethodPost, "/accounts", tc.body, nil)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, tc.wantCode, errorCode(t, rec))
		})
	}
}

func TestGetAccount(t *testing.T) {
	s := newTestServer(t)
	id := s.createAccount(500)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   string
	}{
		{"existing account", fmt.Sprintf("/accounts/%d", id), http.StatusOK, ""},
		{"unknown account", "/accounts/999", http.StatusNotFound, "ACCOUNT_NOT_FOUND"},
		{"non-numeric id", "/accounts/abc", http.StatusBadRequest, "INVALID_ACCOUNT_ID"},
		{"zero id", "/accounts/0", http.StatusBadRequest, "INVALID_ACCOUNT_ID"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.do(http.MethodGet, tc.path, "", nil)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantCode != "" {
				assert.Equal(t, tc.wantCode, errorCode(t, rec))
			}
		})
	}
}

// --- Transfers ---

func TestCreateTransfer_Success(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(10_000), s.createAccount(0)

	rec := s.transfer("k1", transferBody(from, to, 2_550))

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	tr := decode[transferResponse](t, rec)
	assert.NotZero(t, tr.ID)
	assert.Equal(t, int64(2_550), tr.AmountSen)
	assert.Equal(t, "RM 25.50", tr.Amount)
	assert.Equal(t, "test", tr.Reference)
	assert.Empty(t, rec.Header().Get(headerReplayed))
	assert.Equal(t, int64(7_450), s.balance(from))
	assert.Equal(t, int64(2_550), s.balance(to))
}

func TestCreateTransfer_Rejects(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(1_000_000_00), s.createAccount(0)
	poor := s.createAccount(100)

	tests := []struct {
		name       string
		key        string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"missing idempotency key", "", transferBody(from, to, 100), http.StatusBadRequest, "MISSING_IDEMPOTENCY_KEY"},
		{"idempotency key too long", strings.Repeat("k", 65), transferBody(from, to, 100), http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY"},
		{"zero amount", "r1", transferBody(from, to, 0), http.StatusBadRequest, "INVALID_AMOUNT"},
		{"negative amount", "r2", transferBody(from, to, -100), http.StatusBadRequest, "INVALID_AMOUNT"},
		{"decimal amount", "r3", fmt.Sprintf(`{"from_account_id":%d,"to_account_id":%d,"amount_sen":10.5}`, from, to), http.StatusBadRequest, "INVALID_AMOUNT"},
		{"amount as string", "r4", fmt.Sprintf(`{"from_account_id":%d,"to_account_id":%d,"amount_sen":"100"}`, from, to), http.StatusBadRequest, "INVALID_AMOUNT"},
		{"amount overflows int64", "r5", fmt.Sprintf(`{"from_account_id":%d,"to_account_id":%d,"amount_sen":99999999999999999999}`, from, to), http.StatusBadRequest, "INVALID_AMOUNT"},
		{"same account", "r6", transferBody(from, from, 100), http.StatusBadRequest, "SAME_ACCOUNT"},
		{"missing to account", "r7", fmt.Sprintf(`{"from_account_id":%d,"amount_sen":100}`, from), http.StatusBadRequest, "INVALID_ACCOUNT_ID"},
		{"over per-transaction limit", "r8", transferBody(from, to, 5_000_01), http.StatusUnprocessableEntity, "PER_TRANSACTION_LIMIT_EXCEEDED"},
		{"insufficient balance", "r9", transferBody(poor, to, 101), http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS"},
		{"unknown receiver", "r10", transferBody(from, 999, 100), http.StatusNotFound, "ACCOUNT_NOT_FOUND"},
		{"unknown field", "r11", `{"from_account_id":1,"to_account_id":2,"amount_sen":1,"currency":"USD"}`, http.StatusBadRequest, "INVALID_JSON"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.transfer(tc.key, tc.body)

			assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tc.wantCode, errorCode(t, rec))
		})
	}

	assert.Equal(t, int64(1_000_000_00), s.balance(from), "no rejected transfer may move money")
	assert.Equal(t, int64(100), s.balance(poor))
}

func TestCreateTransfer_DailyLimit(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(20_000_00), s.createAccount(0)
	require.Equal(t, http.StatusCreated, s.transfer("d1", transferBody(from, to, 5_000_00)).Code)
	require.Equal(t, http.StatusCreated, s.transfer("d2", transferBody(from, to, 5_000_00)).Code)

	rec := s.transfer("d3", transferBody(from, to, 1))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "DAILY_LIMIT_EXCEEDED", errorCode(t, rec))
}

// --- Idempotency ---

func TestCreateTransfer_ReplaysOriginalResponse(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(10_000), s.createAccount(0)

	first := s.transfer("same-key", transferBody(from, to, 1_000))
	second := s.transfer("same-key", transferBody(from, to, 1_000))

	require.Equal(t, http.StatusCreated, first.Code)
	assert.Equal(t, http.StatusCreated, second.Code)
	assert.Equal(t, "true", second.Header().Get(headerReplayed))
	assert.JSONEq(t, first.Body.String(), second.Body.String(), "replay must be byte-for-byte the original")
	assert.Equal(t, int64(9_000), s.balance(from), "debited exactly once")
}

func TestCreateTransfer_ReplayIgnoresJSONFormatting(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(10_000), s.createAccount(0)
	compact := fmt.Sprintf(`{"from_account_id":%d,"to_account_id":%d,"amount_sen":100,"reference":"x"}`, from, to)
	reordered := fmt.Sprintf(`{ "reference": "x",  "amount_sen": 100, "to_account_id": %d, "from_account_id": %d }`, to, from)

	require.Equal(t, http.StatusCreated, s.transfer("fmt-key", compact).Code)
	rec := s.transfer("fmt-key", reordered)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "true", rec.Header().Get(headerReplayed))
}

// A business rejection is a final outcome and is replayed too. Otherwise a
// client retrying after a timeout could see a different result.
func TestCreateTransfer_ReplaysBusinessRejection(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(100), s.createAccount(0)

	first := s.transfer("poor-key", transferBody(from, to, 500))
	second := s.transfer("poor-key", transferBody(from, to, 500))

	assert.Equal(t, http.StatusUnprocessableEntity, first.Code)
	assert.Equal(t, http.StatusUnprocessableEntity, second.Code)
	assert.Equal(t, "true", second.Header().Get(headerReplayed))
	assert.Equal(t, "INSUFFICIENT_FUNDS", errorCode(t, second))
}

func TestCreateTransfer_KeyReusedWithDifferentBody(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(10_000), s.createAccount(0)
	require.Equal(t, http.StatusCreated, s.transfer("reuse", transferBody(from, to, 100)).Code)

	rec := s.transfer("reuse", transferBody(from, to, 200))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "IDEMPOTENCY_KEY_REUSED", errorCode(t, rec))
	assert.Equal(t, int64(9_900), s.balance(from))
}

func TestCreateTransfer_RequestStillInProgress(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(10_000), s.createAccount(0)
	// Simulate a concurrent request that has reserved the key but not finished.
	fp := fingerprint(bank.TransferRequest{FromAccountID: from, ToAccountID: to, Amount: 100, Reference: "test"})
	_, _, err := s.idem.Begin(context.Background(), "busy", fp)
	require.NoError(t, err)

	rec := s.transfer("busy", transferBody(from, to, 100))

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "REQUEST_IN_PROGRESS", errorCode(t, rec))
	assert.Equal(t, "1", rec.Header().Get("Retry-After"))
	assert.Equal(t, int64(10_000), s.balance(from))
}

func TestCreateTransfer_ValidationErrorDoesNotConsumeKey(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(10_000), s.createAccount(0)

	bad := s.transfer("fix-me", transferBody(from, to, 0))
	good := s.transfer("fix-me", transferBody(from, to, 100))

	assert.Equal(t, http.StatusBadRequest, bad.Code)
	assert.Equal(t, http.StatusCreated, good.Code)
	assert.Empty(t, good.Header().Get(headerReplayed), "corrected request is executed, not replayed")
}

// failingStore simulates a database outage during a transfer. Embedding
// *memstore.Store gives it all of memstore's methods; it then overrides
// just InTransferTx.
type failingStore struct {
	*memstore.Store
}

func (failingStore) InTransferTx(context.Context, int64, int64, func(bank.TransferTx, bank.Account, bank.Account) error) error {
	return errors.New("connection refused")
}

func TestCreateTransfer_InternalErrorReleasesKey(t *testing.T) {
	s := newTestServerWithStore(t, failingStore{memstore.New()})
	body := transferBody(1, 2, 100)

	rec := s.transfer("retry-after-outage", body)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_ERROR", errorCode(t, rec))
	assert.NotContains(t, rec.Body.String(), "connection refused", "internal details must not leak")

	_, reserved, err := s.idem.Begin(context.Background(), "retry-after-outage", "any")
	require.NoError(t, err)
	assert.True(t, reserved, "key must be released so the client can retry")
}

// --- Transaction history ---

func TestListTransactions(t *testing.T) {
	s := newTestServer(t)
	from, to := s.createAccount(10_000), s.createAccount(0)
	for i, amount := range []int64{100, 200, 300} {
		require.Equal(t, http.StatusCreated, s.transfer(fmt.Sprintf("h%d", i), transferBody(from, to, amount)).Code)
	}

	rec := s.do(http.MethodGet, fmt.Sprintf("/accounts/%d/transactions?limit=2", from), "", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	resp := decode[transactionsResponse](t, rec)
	assert.Equal(t, from, resp.AccountID)
	require.Len(t, resp.Transactions, 2)
	assert.Equal(t, int64(300), resp.Transactions[0].AmountSen, "newest first")
	assert.Equal(t, "DEBIT", resp.Transactions[0].Direction)
	assert.Equal(t, int64(9_400), resp.Transactions[0].BalanceAfterSen)
	assert.Equal(t, to, resp.Transactions[0].CounterpartyAccountID)
}

func TestListTransactions_EmptyHistoryIsEmptyArray(t *testing.T) {
	s := newTestServer(t)
	id := s.createAccount(0)

	rec := s.do(http.MethodGet, fmt.Sprintf("/accounts/%d/transactions", id), "", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, fmt.Sprintf(`{"account_id":%d,"transactions":[]}`, id), rec.Body.String())
}

func TestListTransactions_Rejects(t *testing.T) {
	s := newTestServer(t)
	id := s.createAccount(0)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   string
	}{
		{"unknown account", "/accounts/999/transactions", http.StatusNotFound, "ACCOUNT_NOT_FOUND"},
		{"limit zero", fmt.Sprintf("/accounts/%d/transactions?limit=0", id), http.StatusBadRequest, "INVALID_LIMIT"},
		{"limit too large", fmt.Sprintf("/accounts/%d/transactions?limit=101", id), http.StatusBadRequest, "INVALID_LIMIT"},
		{"limit not a number", fmt.Sprintf("/accounts/%d/transactions?limit=ten", id), http.StatusBadRequest, "INVALID_LIMIT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.do(http.MethodGet, tc.path, "", nil)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantCode, errorCode(t, rec))
		})
	}
}

// --- Health ---

func TestHealth(t *testing.T) {
	healthy := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("redis down") }

	ok := newTestServerWithStore(t, memstore.New(), healthy).do(http.MethodGet, "/healthz", "", nil)
	assert.Equal(t, http.StatusOK, ok.Code)

	unavailable := newTestServerWithStore(t, memstore.New(), healthy, down).do(http.MethodGet, "/healthz", "", nil)
	assert.Equal(t, http.StatusServiceUnavailable, unavailable.Code)
}
