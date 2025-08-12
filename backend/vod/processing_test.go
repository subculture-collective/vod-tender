package vod

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

type mockDownloader struct{ path string; err error }
func (m mockDownloader) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) { return m.path, m.err }

type mockUploader struct{ url string; err error }
func (m mockUploader) Upload(ctx context.Context, path, title string, date time.Time) (string, error) { return m.url, m.err }

func TestParseTwitchDuration(t *testing.T) {
    cases := map[string]int{"1h2m3s":3723, "45m":2700, "30s":30, "2h":7200, "":0}
    for in, want := range cases {
        got := parseTwitchDuration(in)
        if got != want { t.Fatalf("%s => %d want %d", in, got, want) }
    }
}

func TestProcessOnceHappyPath(t *testing.T) {
    // Setup in-memory DB
    dsn := "file:proc_test?mode=memory&cache=shared"
    db, err := sql.Open("sqlite3", dsn)
    if err != nil { t.Fatal(err) }
    defer db.Close()
    _, _ = db.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT, updated_at TEXT);`)
    _, _ = db.Exec(`CREATE TABLE vods (twitch_vod_id TEXT PRIMARY KEY, title TEXT, date TIMESTAMP, duration_seconds INT, downloaded_path TEXT, youtube_url TEXT, processed INT DEFAULT 0, processing_error TEXT, download_retries INT, priority INT, created_at TEXT, updated_at TEXT);`)
    // Insert a VOD to process
    _, _ = db.Exec(`INSERT INTO vods (twitch_vod_id,title,date,duration_seconds,created_at) VALUES ('123','Test',strftime('%Y-%m-%dT%H:%M:%fZ','now'),60,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`)

    // Override globals
    oldD, oldU := downloader, uploader
    downloader = mockDownloader{path: "/tmp/123.mp4"}
    uploader = mockUploader{url: "https://youtu.be/abc"}
    defer func(){ downloader, uploader = oldD, oldU }()

    ctx, cancel := context.WithCancel(context.Background()); defer cancel()
    if err := processOnce(ctx, db); err != nil { t.Fatal(err) }
    var processed int
    var yt string
    _ = db.QueryRow(`SELECT processed,youtube_url FROM vods WHERE twitch_vod_id='123'`).Scan(&processed, &yt)
    if processed != 1 || yt == "" { t.Fatalf("expected processed=1 and youtube_url set got %d %s", processed, yt) }
}

func TestProcessOnceDownloadFail(t *testing.T) {
    dsn := "file:proc_test2?mode=memory&cache=shared"
    db, err := sql.Open("sqlite3", dsn)
    if err != nil { t.Fatal(err) }
    defer db.Close()
    _, _ = db.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT, updated_at TEXT);`)
    _, _ = db.Exec(`CREATE TABLE vods (twitch_vod_id TEXT PRIMARY KEY, title TEXT, date TIMESTAMP, duration_seconds INT, downloaded_path TEXT, youtube_url TEXT, processed INT DEFAULT 0, processing_error TEXT, download_retries INT, priority INT, created_at TEXT, updated_at TEXT);`)
    _, _ = db.Exec(`INSERT INTO vods (twitch_vod_id,title,date,duration_seconds,created_at) VALUES ('d1','D','2024-01-01T00:00:00Z',30,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`)
    oldD, oldU := downloader, uploader
    downloader = mockDownloader{err: errors.New("boom")}
    uploader = mockUploader{url: "ignored"}
    defer func(){ downloader, uploader = oldD, oldU }()
    ctx, cancel := context.WithCancel(context.Background()); defer cancel()
    if err := processOnce(ctx, db); err != nil { t.Fatal(err) }
    var perr string
    _ = db.QueryRow(`SELECT processing_error FROM vods WHERE twitch_vod_id='d1'`).Scan(&perr)
    if perr == "" { t.Fatalf("expected processing_error set") }
}
