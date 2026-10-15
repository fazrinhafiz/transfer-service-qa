// Package httpapi exposes the bank service over HTTP/JSON.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/idempotency"
)

// HealthCheck reports whether a dependency (MySQL, Redis) is reachable.
type HealthCheck func(ctx context.Context) error

type Handler struct {
	svc    *bank.Service
	idem   idempotency.Store
	log    *slog.Logger
	checks []HealthCheck
}

func NewRouter(svc *bank.Service, idem idempotency.Store, log *slog.Logger, checks ...HealthCheck) http.Handler {
	h := &Handler{svc: svc, idem: idem, log: log, checks: checks}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(log))
	r.Use(middleware.Recoverer) // turns a panic into a 500 instead of crashing the server

	r.Get("/healthz", h.health)
	r.Post("/accounts", h.createAccount)
	r.Get("/accounts/{id}", h.getAccount)
	r.Get("/accounts/{id}/transactions", h.listTransactions)
	r.Post("/transfers", h.createTransfer)
	return r
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	for _, check := range h.checks {
		if err := check(ctx); err != nil {
			h.log.Warn("health check failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// requestLogger is middleware: a function that wraps a handler and returns
// a new handler that does something before and after calling it.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Debug("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
