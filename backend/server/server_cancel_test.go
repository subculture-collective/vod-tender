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

func TestCancelNoActive(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()
	if err := dbpkg.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	handlers := NewHandlers(context.Background(), db)
	req := httptest.NewRequest(http.MethodPost, "/vods/abc/cancel", nil)
	rr := httptest.NewRecorder()
	handlers.handleVodCancel(rr, req, "abc")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 when nothing to cancel, got %d", rr.Code)
	}
}
