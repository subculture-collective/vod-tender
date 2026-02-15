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
	err  error
	path string
}

func (m mockDownloader) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) {
	return m.path, m.err
}

type mockUploader struct {
	err error
	url string
}

func (m mockUploader) Upload(ctx context.Context, dbc *sql.DB, path, title string, date time.Time) (string, error) {
	return m.url, m.err
}

// spyDownloader implements Downloader and increments a counter when invoked.
type spyDownloader struct{ called *int }

func (s spyDownloader) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) {
	if s.called != nil {
		*s.called++
	}
	return "/tmp/" + id + ".mp4", nil
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
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()
	if err := dbpkg.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	channel := ""
	_, _ = db.ExecContext(context.Background(), `INSERT INTO vods (channel,twitch_vod_id,title,date,duration_seconds,created_at) VALUES ($1,'123','Test',NOW(),60,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, channel)
	oldD, oldU := downloader, uploader
	downloader = mockDownloader{path: "/tmp/123.mp4"}
	uploader = mockUploader{url: "https://youtu.be/abc"}
	defer func() { downloader, uploader = oldD, oldU }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := processOnce(ctx, db, channel); err != nil {
		t.Fatal(err)
	}
	var processed bool
	var yt string
	_ = db.QueryRowContext(context.Background(), `SELECT processed,youtube_url FROM vods WHERE twitch_vod_id='123'`).Scan(&processed, &yt)
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
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()
	if err := dbpkg.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	channel := ""
	_, _ = db.ExecContext(context.Background(), `INSERT INTO vods (channel,twitch_vod_id,title,date,duration_seconds,created_at) VALUES ($1,'d1','D','2024-01-01T00:00:00Z',30,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, channel)
	oldD, oldU := downloader, uploader
	downloader = mockDownloader{err: errors.New("boom")}
	uploader = mockUploader{url: "ignored"}
	defer func() { downloader, uploader = oldD, oldU }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := processOnce(ctx, db, channel); err != nil {
		t.Fatal(err)
	}
	var perr string
	_ = db.QueryRowContext(context.Background(), `SELECT processing_error FROM vods WHERE twitch_vod_id='d1'`).Scan(&perr)
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
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()
	if err := dbpkg.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("CIRCUIT_FAILURE_THRESHOLD", "2"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("CIRCUIT_FAILURE_THRESHOLD"); err != nil {
			t.Errorf("failed to unset env: %v", err)
		}
	}()
	ctx := context.Background()
	channel := ""
	updateCircuitOnFailure(ctx, db, channel)
	var v string
	_ = db.QueryRowContext(context.Background(), `SELECT value FROM kv WHERE channel=$1 AND key='circuit_failures'`, channel).Scan(&v)
	if v != "1" {
		t.Fatalf("expected failures=1 got %s", v)
	}
	updateCircuitOnFailure(ctx, db, channel)
	_ = db.QueryRowContext(context.Background(), `SELECT value FROM kv WHERE channel=$1 AND key='circuit_state'`, channel).Scan(&v)
	if v != "open" {
		t.Fatalf("expected state open got %s", v)
	}
	resetCircuit(ctx, db, channel)
	_ = db.QueryRowContext(context.Background(), `SELECT value FROM kv WHERE channel=$1 AND key='circuit_state'`, channel).Scan(&v)
	if v != "closed" {
		t.Fatalf("expected state closed got %s", v)
	}
}

func TestCircuitBreakerHalfOpenSuccess(t *testing.T) {
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
	ctx := context.Background()
	channel := "test-half-open-success"

	// Set circuit to half-open state
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (channel,key,value,updated_at) VALUES ($1,'circuit_state','half-open',NOW())
		ON CONFLICT(channel,key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, channel)

	// Success in half-open should close the circuit
	resetCircuit(ctx, db, channel)

	var state string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel=$1 AND key='circuit_state'`, channel).Scan(&state)
	if state != "closed" {
		t.Fatalf("expected state closed after half-open success, got %s", state)
	}

	// Verify failures are reset
	var failures string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel=$1 AND key='circuit_failures'`, channel).Scan(&failures)
	if failures != "0" {
		t.Fatalf("expected failures=0 after reset, got %s", failures)
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
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
	if err := os.Setenv("CIRCUIT_FAILURE_THRESHOLD", "2"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("CIRCUIT_FAILURE_THRESHOLD"); err != nil {
			t.Errorf("failed to unset env: %v", err)
		}
	}()
	ctx := context.Background()
	channel := "test-half-open-failure"

	// Set circuit to half-open state
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (channel,key,value,updated_at) VALUES ($1,'circuit_state','half-open',NOW())
		ON CONFLICT(channel,key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, channel)

	// Failure in half-open should reopen the circuit immediately
	updateCircuitOnFailure(ctx, db, channel)

	var state string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel=$1 AND key='circuit_state'`, channel).Scan(&state)
	if state != "open" {
		t.Fatalf("expected state open after half-open failure, got %s", state)
	}

	// Verify circuit_open_until is set
	var until string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel=$1 AND key='circuit_open_until'`, channel).Scan(&until)
	if until == "" {
		t.Fatal("expected circuit_open_until to be set after reopening from half-open")
	}
}

func TestUploadDailyLimitCapStopsProcessing(t *testing.T) {
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
	channel := "cap-test"
	// Seed one successfully uploaded VOD within the last 24h (counts toward the cap)
	_, _ = db.ExecContext(context.Background(), `INSERT INTO vods (channel,twitch_vod_id,title,date,duration_seconds,created_at,updated_at,processed,youtube_url)
		VALUES ($1,'u1','Uploaded',NOW(),60,NOW(),NOW(),TRUE,'https://youtu.be/ok')
		ON CONFLICT (twitch_vod_id) DO UPDATE SET youtube_url=EXCLUDED.youtube_url, updated_at=EXCLUDED.updated_at, processed=EXCLUDED.processed`, channel)
	// Candidate VOD that should NOT be processed due to the cap
	_, _ = db.ExecContext(context.Background(), `INSERT INTO vods (channel,twitch_vod_id,title,date,duration_seconds,created_at,processed)
		VALUES ($1,'c1','Candidate',NOW(),60,NOW(),FALSE)
		ON CONFLICT (twitch_vod_id) DO NOTHING`, channel)

	// Set global upload cap to 1 so we hit the limit and skip processing
	if err := os.Setenv("UPLOAD_DAILY_LIMIT", "1"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("UPLOAD_DAILY_LIMIT"); err != nil {
			t.Errorf("failed to unset env: %v", err)
		}
	}()

	// Spy downloader to detect unexpected invocations
	var called int
	sdown := spyDownloader{called: &called}
	oldD, oldU := downloader, uploader
	downloader = sdown
	uploader = mockUploader{url: "https://youtu.be/ignored"}
	defer func() { downloader, uploader = oldD, oldU }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := processOnce(ctx, db, channel); err != nil {
		t.Fatal(err)
	}

	// Assert candidate remains unprocessed and no download attempt occurred
	var processed bool
	var yt string
	var dlPath sql.NullString
	_ = db.QueryRowContext(context.Background(), `SELECT processed,youtube_url,downloaded_path FROM vods WHERE twitch_vod_id='c1'`).Scan(&processed, &yt, &dlPath)
	if processed || yt != "" || (dlPath.Valid && dlPath.String != "") {
		t.Fatalf("expected candidate to remain unprocessed with no youtube_url or downloaded_path; got processed=%v youtube_url=%q downloaded_path.Valid=%v", processed, yt, dlPath.Valid)
	}
	if called != 0 {
		t.Fatalf("expected downloader not to be called when cap reached; called=%d", called)
	}
}
