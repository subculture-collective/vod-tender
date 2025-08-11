package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newTestDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }
    return db
}

func TestHealthzOK(t *testing.T) {
    db := newTestDB(t)
    t.Cleanup(func() { db.Close() })

    req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
    rr := httptest.NewRecorder()

    h := NewMux(db)
    h.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
    }
    if got := rr.Body.String(); got != "ok" {
        t.Fatalf("expected ok body, got %q", got)
    }
}

func TestStartAndShutdown(t *testing.T) {
    db := newTestDB(t)
    t.Cleanup(func() { db.Close() })
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Run server in background on random port by using :0
    done := make(chan error, 1)
    go func() { done <- Start(ctx, db, ":0") }()

    cancel()
    if err := <-done; err != nil {
        t.Fatalf("server returned error: %v", err)
    }
}
