// Command api runs the transfer service HTTP server.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers the "mysql" driver with database/sql
	"github.com/redis/go-redis/v9"

	"github.com/fazrinhafiz/transfer-service-qa/service/internal/bank"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/config"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/httpapi"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/idempotency"
	"github.com/fazrinhafiz/transfer-service-qa/service/internal/store/mysqlstore"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// run holds the real startup logic and returns errors instead of exiting,
// which keeps main tiny and makes cleanup (defers) run reliably.
func run() error {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	// ctx is cancelled on Ctrl+C or `docker stop` (SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(50)
	db.SetConnMaxLifetime(5 * time.Minute)

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	if err := waitFor(ctx, log, "mysql", db.PingContext); err != nil {
		return err
	}
	if err := waitFor(ctx, log, "redis", func(ctx context.Context) error { return rdb.Ping(ctx).Err() }); err != nil {
		return err
	}

	svc := bank.NewService(mysqlstore.New(db))
	router := httpapi.NewRouter(svc, idempotency.NewRedisStore(rdb), log,
		db.PingContext,
		func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// The server runs in its own goroutine (a lightweight thread) so this
	// function can wait for a shutdown signal at the same time. Errors are
	// sent back over a channel.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		serverErr <- srv.ListenAndServe()
	}()

	// select waits until one of the channels receives a value.
	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	// Graceful shutdown: stop accepting connections and let in-flight
	// transfers finish, for up to 10 seconds.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// waitFor retries ping until it succeeds, so the API can start before MySQL
// has finished initialising.
func waitFor(ctx context.Context, log *slog.Logger, name string, ping func(context.Context) error) error {
	const attempts = 30
	for i := 1; ; i++ {
		err := ping(ctx)
		if err == nil {
			return nil
		}
		if i == attempts {
			return fmt.Errorf("%s not reachable after %d attempts: %w", name, attempts, err)
		}
		log.Info("waiting for dependency", "name", name, "attempt", i, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
