package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestReprocess(t *testing.T) {
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil { t.Fatal(err) }
    defer db.Close()

    // minimal schema to allow update
    _, err = db.Exec(`CREATE TABLE vods (twitch_vod_id TEXT UNIQUE, processed BOOLEAN, processing_error TEXT, youtube_url TEXT, downloaded_path TEXT, download_state TEXT, download_retries INTEGER, download_bytes INTEGER, download_total INTEGER, progress_updated_at DATETIME, updated_at DATETIME)`)
    if err != nil { t.Fatal(err) }
    _, err = db.Exec(`INSERT INTO vods (twitch_vod_id, processed, processing_error, youtube_url, downloaded_path, download_state, download_retries, download_bytes, download_total) VALUES ('123', 1, 'err', 'yt', 'path', 'state', 2, 100, 200)`)
    if err != nil { t.Fatal(err) }

    req := httptest.NewRequest(http.MethodPost, "/vods/123/reprocess", nil)
    rr := httptest.NewRecorder()
    handleVodReprocess(rr, req, db, "123")
    if rr.Code != http.StatusNoContent {
        t.Fatalf("expected 204, got %d", rr.Code)
    }

    row := db.QueryRow(`SELECT processed, processing_error, youtube_url, downloaded_path, download_state, download_retries, download_bytes, download_total FROM vods WHERE twitch_vod_id='123'`)
    var processed bool
    var processingError, youtubeURL, downloadedPath, downloadState *string
    var retries, bytes, total int
    if err := row.Scan(&processed, &processingError, &youtubeURL, &downloadedPath, &downloadState, &retries, &bytes, &total); err != nil {
        t.Fatal(err)
    }
    if processed {
        t.Fatalf("expected processed=0 after reset")
    }
    if retries != 0 || bytes != 0 || total != 0 {
        t.Fatalf("expected counters reset, got retries=%d bytes=%d total=%d", retries, bytes, total)
    }
}
