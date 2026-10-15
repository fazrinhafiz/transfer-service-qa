package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/money"
)

type createAccountRequest struct {
	OwnerName         string `json:"owner_name"`
	InitialBalanceSen int64  `json:"initial_balance_sen"`
}

// Struct tags (the `json:"..."` strings) tell encoding/json which key to
// use for each field. Amounts are sent as integer sen plus a display string.
type accountResponse struct {
	ID         int64     `json:"id"`
	OwnerName  string    `json:"owner_name"`
	BalanceSen int64     `json:"balance_sen"`
	Balance    string    `json:"balance"`
	CreatedAt  time.Time `json:"created_at"`
}

func newAccountResponse(a bank.Account) accountResponse {
	return accountResponse{
		ID:         a.ID,
		OwnerName:  a.OwnerName,
		BalanceSen: int64(a.Balance),
		Balance:    a.Balance.String(),
		CreatedAt:  a.CreatedAt,
	}
}

type ledgerEntryResponse struct {
	TransferID            int64     `json:"transfer_id"`
	Direction             string    `json:"direction"`
	AmountSen             int64     `json:"amount_sen"`
	Amount                string    `json:"amount"`
	BalanceAfterSen       int64     `json:"balance_after_sen"`
	CounterpartyAccountID int64     `json:"counterparty_account_id"`
	Reference             string    `json:"reference"`
	CreatedAt             time.Time `json:"created_at"`
}

type transactionsResponse struct {
	AccountID    int64                 `json:"account_id"`
	Transactions []ledgerEntryResponse `json:"transactions"`
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	var body createAccountRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}
	acc, err := h.svc.CreateAccount(r.Context(), bank.CreateAccountRequest{
		OwnerName:      body.OwnerName,
		InitialBalance: money.Sen(body.InitialBalanceSen),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/accounts/"+strconv.FormatInt(acc.ID, 10))
	writeJSON(w, http.StatusCreated, newAccountResponse(acc))
}

func (h *Handler) getAccount(w http.ResponseWriter, r *http.Request) {
	id, err := accountIDParam(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	acc, err := h.svc.GetAccount(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newAccountResponse(acc))
}

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	id, err := accountIDParam(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	limit := 0 // service default
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			h.writeError(w, r, errInvalidLimit)
			return
		}
	}

	entries, err := h.svc.History(r.Context(), id, limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	resp := transactionsResponse{AccountID: id, Transactions: make([]ledgerEntryResponse, 0, len(entries))}
	for _, e := range entries {
		resp.Transactions = append(resp.Transactions, ledgerEntryResponse{
			TransferID:            e.TransferID,
			Direction:             string(e.Direction),
			AmountSen:             int64(e.Amount),
			Amount:                e.Amount.String(),
			BalanceAfterSen:       int64(e.BalanceAfter),
			CounterpartyAccountID: e.CounterpartyAccountID,
			Reference:             e.Reference,
			CreatedAt:             e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func accountIDParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, bank.ErrInvalidAccountID
	}
	return id, nil
}
