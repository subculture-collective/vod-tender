package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
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
