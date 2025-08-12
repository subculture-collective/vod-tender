package vod

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/onnwee/vod-tender/backend/config"
	"github.com/onnwee/vod-tender/backend/db"
	youtubeapi "github.com/onnwee/vod-tender/backend/youtubeapi"
)

// Downloader abstracts video retrieval (for tests/mocks).
type Downloader interface { Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) }

// Uploader abstracts upload destination behavior.
type Uploader interface { Upload(ctx context.Context, path, title string, date time.Time) (string, error) }

// default implementations wrap existing functions.
type ytDLPDownloader struct{}
func (ytDLPDownloader) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) { return downloadVOD(ctx, dbc, id, dataDir) }

type youtubeUploader struct{}
func (youtubeUploader) Upload(ctx context.Context, path, title string, date time.Time) (string, error) { return uploadToYouTube(ctx, path, title, date) }

// configurable for tests
var (
	downloader Downloader = ytDLPDownloader{}
	uploader   Uploader   = youtubeUploader{}
)

// StartVODProcessingJob runs a loop processing VODs at an interval.
func StartVODProcessingJob(ctx context.Context, dbc *sql.DB) {
	interval := 1 * time.Minute
	if s := os.Getenv("VOD_PROCESS_INTERVAL"); s != "" { if d, err := time.ParseDuration(s); err == nil && d > 0 { interval = d } }
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("vod processing job stopped")
			return
		case <-ticker.C:
			if err := processOnce(ctx, dbc); err != nil { slog.Warn("process once", slog.Any("err", err)) }
		}
	}
}

// processOnce selects a single pending VOD and processes it.
func processOnce(ctx context.Context, dbc *sql.DB) error {
	_, _ = dbc.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('job_vod_process_last', strftime('%Y-%m-%dT%H:%M:%fZ','now'), CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`)
	var state, until string
	_ = dbc.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&state)
	if state == "open" {
		_ = dbc.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&until)
		if until != "" {
			if t, err := time.Parse(time.RFC3339, until); err == nil {
				if time.Now().Before(t) { return nil }
				_, _ = dbc.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_state','half-open',CURRENT_TIMESTAMP)
					ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`)
			}
		}
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" { dataDir = "data" }
	if err := os.MkdirAll(dataDir, 0o755); err != nil { return fmt.Errorf("mkdir data dir: %w", err) }
	if err := DiscoverAndUpsert(ctx, dbc); err != nil { return err }
	maxAttempts := 5
	if s := os.Getenv("DOWNLOAD_MAX_ATTEMPTS"); s != "" { if n, err := strconv.Atoi(s); err == nil && n > 0 { maxAttempts = n } }
	cooldown := 600 * time.Second
	if s := os.Getenv("PROCESSING_RETRY_COOLDOWN"); s != "" { if d, err := time.ParseDuration(s); err == nil && d > 0 { cooldown = d } }
	row := dbc.QueryRow(`SELECT twitch_vod_id, title, date FROM vods
		WHERE processed=0 AND (
			processing_error IS NULL OR processing_error='' OR (download_retries < ? AND (strftime('%s','now') - strftime('%s', COALESCE(updated_at, created_at))) >= ?)
		)
		ORDER BY priority DESC, date ASC LIMIT 1`, maxAttempts, int(cooldown.Seconds()))
	var id, title string
	var date time.Time
	if err := row.Scan(&id, &title, &date); err != nil {
		if err == sql.ErrNoRows { return nil }
		return err
	}
	filePath, err := downloader.Download(ctx, dbc, id, dataDir)
	if err != nil {
		slog.Error("download failed", slog.String("vod_id", id), slog.Any("err", err))
		_, _ = dbc.Exec(`UPDATE vods SET processing_error=?, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, err.Error(), id)
		updateCircuitOnFailure(ctx, dbc)
		return nil
	}
	resetCircuit(ctx, dbc)
	_, _ = dbc.Exec(`UPDATE vods SET downloaded_path=?, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, filePath, id)
	ytURL, err := uploader.Upload(ctx, filePath, title, date)
	if err != nil {
		slog.Error("upload failed", slog.String("vod_id", id), slog.Any("err", err))
		_, _ = dbc.Exec(`UPDATE vods SET processing_error=?, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, err.Error(), id)
		return nil
	}
	_, _ = dbc.Exec(`UPDATE vods SET youtube_url=?, processed=1, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, ytURL, id)
	slog.Info("processed vod", slog.String("vod_id", id), slog.String("youtube_url", ytURL))
	return nil
}

// uploadToYouTube uploads the given video file using stored OAuth token.
func uploadToYouTube(ctx context.Context, path, title string, date time.Time) (string, error) {
	adb, _ := sql.Open("sqlite3", os.Getenv("DB_DSN"))
	defer adb.Close()
	ts := &db.TokenStoreAdapter{DB: adb}
	cfg, _ := config.Load()
	yts := youtubeapi.New(cfg, ts)
	svc, err := yts.Client(ctx)
	if err != nil { return "", fmt.Errorf("youtube client: %w", err) }
	datePrefix := date.Format("2006-01-02")
	finalTitle := fmt.Sprintf("%s %s", datePrefix, title)
	description := fmt.Sprintf("Uploaded from Twitch VOD on %s", date.Format(time.RFC3339))
	return youtubeapi.UploadVideo(ctx, svc, path, finalTitle, description, "private")
}
