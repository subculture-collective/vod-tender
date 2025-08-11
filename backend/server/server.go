package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"
)

// NewMux returns the HTTP handler with all routes.
func NewMux(db *sql.DB) http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        if err := db.PingContext(r.Context()); err != nil {
            http.Error(w, "unhealthy", http.StatusServiceUnavailable)
            return
        }
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })
    return mux
}

// Start runs the HTTP server and shuts down gracefully on context cancellation.
func Start(ctx context.Context, db *sql.DB, addr string) error {
    srv := &http.Server{
        Addr:         addr,
        Handler:      NewMux(db),
        ReadTimeout:  5 * time.Second,
        WriteTimeout: 10 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    // Shutdown goroutine
    go func() {
        <-ctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if err := srv.Shutdown(shutdownCtx); err != nil {
            slog.Error("http server shutdown error", slog.Any("err", err))
        }
    }()

    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        slog.Error("http server error", slog.Any("err", err))
        return err
    }
    return nil
}
