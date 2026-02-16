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

func TestReprocess(t *testing.T) {
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
	_, err = db.ExecContext(context.Background(), `INSERT INTO vods (twitch_vod_id, processed, processing_error, youtube_url, downloaded_path, download_state, download_retries, download_bytes, download_total, created_at) VALUES ('123', TRUE, 'err', 'yt', 'path', 'state', 2, 100, 200, NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}

	handlers := NewHandlers(context.Background(), db)
	req := httptest.NewRequest(http.MethodPost, "/vods/123/reprocess", nil)
	rr := httptest.NewRecorder()
	handlers.handleVodReprocess(rr, req, "123")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}

	row := db.QueryRowContext(context.Background(), `SELECT processed, processing_error, youtube_url, downloaded_path, download_state, download_retries, download_bytes, download_total FROM vods WHERE twitch_vod_id='123'`)
	var processed bool
	var processingError, youtubeURL, downloadedPath, downloadState *string
	var retries, bytes, total int
	if err := row.Scan(&processed, &processingError, &youtubeURL, &downloadedPath, &downloadState, &retries, &bytes, &total); err != nil {
		t.Fatal(err)
	}
	if processed {
		t.Fatalf("expected processed=false after reset")
	}
	if retries != 0 || bytes != 0 || total != 0 {
		t.Fatalf("expected counters reset, got retries=%d bytes=%d total=%d", retries, bytes, total)
	}
}
