package vod

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

type mockDownloader struct {
	path string
	err  error
}

func (m mockDownloader) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) {
	return m.path, m.err
}

type mockUploader struct {
	url string
	err error
}

func (m mockUploader) Upload(ctx context.Context, path, title string, date time.Time) (string, error) {
	return m.url, m.err
}

func TestParseTwitchDuration(t *testing.T) {
	cases := map[string]int{"1h2m3s": 3723, "45m": 2700, "30s": 30, "2h": 7200, "": 0}
	for in, want := range cases {
		if got := parseTwitchDuration(in); got != want {
			t.Fatalf("%s => %d want %d", in, got, want)
		}
	}
}

func TestProcessOnceHappyPath(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`INSERT INTO vods (twitch_vod_id,title,date,duration_seconds,created_at) VALUES ('123','Test',NOW(),60,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`)
	oldD, oldU := downloader, uploader
	downloader = mockDownloader{path: "/tmp/123.mp4"}
	uploader = mockUploader{url: "https://youtu.be/abc"}
	defer func() { downloader, uploader = oldD, oldU }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := processOnce(ctx, db); err != nil {
		t.Fatal(err)
	}
	var processed bool
	var yt string
	_ = db.QueryRow(`SELECT processed,youtube_url FROM vods WHERE twitch_vod_id='123'`).Scan(&processed, &yt)
	if !processed || yt == "" {
		t.Fatalf("expected processed=true and youtube_url set got %v %s", processed, yt)
	}
}

func TestProcessOnceDownloadFail(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`INSERT INTO vods (twitch_vod_id,title,date,duration_seconds,created_at) VALUES ('d1','D','2024-01-01T00:00:00Z',30,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`)
	oldD, oldU := downloader, uploader
	downloader = mockDownloader{err: errors.New("boom")}
	uploader = mockUploader{url: "ignored"}
	defer func() { downloader, uploader = oldD, oldU }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := processOnce(ctx, db); err != nil {
		t.Fatal(err)
	}
	var perr string
	_ = db.QueryRow(`SELECT processing_error FROM vods WHERE twitch_vod_id='d1'`).Scan(&perr)
	if perr == "" {
		t.Fatalf("expected processing_error set")
	}
}

func TestCircuitBreakerTransitions(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatal(err)
	}
	os.Setenv("CIRCUIT_FAILURE_THRESHOLD", "2")
	defer os.Unsetenv("CIRCUIT_FAILURE_THRESHOLD")
	ctx := context.Background()
	updateCircuitOnFailure(ctx, db)
	var v string
	_ = db.QueryRow(`SELECT value FROM kv WHERE key='circuit_failures'`).Scan(&v)
	if v != "1" {
		t.Fatalf("expected failures=1 got %s", v)
	}
	updateCircuitOnFailure(ctx, db)
	_ = db.QueryRow(`SELECT value FROM kv WHERE key='circuit_state'`).Scan(&v)
	if v != "open" {
		t.Fatalf("expected state open got %s", v)
	}
	resetCircuit(ctx, db)
	_ = db.QueryRow(`SELECT value FROM kv WHERE key='circuit_state'`).Scan(&v)
	if v != "closed" {
		t.Fatalf("expected state closed got %s", v)
	}
}
