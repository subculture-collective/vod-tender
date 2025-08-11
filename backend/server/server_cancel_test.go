package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestCancelNoActive(t *testing.T) {
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil { t.Fatal(err) }
    defer db.Close()

    req := httptest.NewRequest(http.MethodPost, "/vods/abc/cancel", nil)
    rr := httptest.NewRecorder()
    handleVodCancel(rr, req, db, "abc")
    if rr.Code != http.StatusNoContent {
        t.Fatalf("expected 204 when nothing to cancel, got %d", rr.Code)
    }
}
