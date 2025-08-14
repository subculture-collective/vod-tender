package vod

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/onnwee/vod-tender/backend/config"
	"github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/telemetry"
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
	slog.Info("vod processing job starting", slog.Duration("interval", interval))
	// Kick an immediate run so we don't wait a full interval after boot.
	if err := processOnce(ctx, dbc); err != nil { slog.Warn("process once", slog.Any("err", err)) }
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
	_, _ = dbc.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('job_vod_process_last', to_char(NOW() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), NOW())
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`)
	var state, until string
	_ = dbc.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&state)
	if state == "open" {
		_ = dbc.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&until)
		if until != "" {
			if t, err := time.Parse(time.RFC3339, until); err == nil {
				if time.Now().Before(t) {
					slog.Debug("circuit open; skipping processing cycle", slog.String("until", until))
					return nil
				}
				_, _ = dbc.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_state','half-open',CURRENT_TIMESTAMP)
					ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`)
				slog.Info("circuit transitioning to half-open")
			}
		}
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" { dataDir = "data" }
	if err := os.MkdirAll(dataDir, 0o755); err != nil { return fmt.Errorf("mkdir data dir: %w", err) }
	if err := DiscoverAndUpsert(ctx, dbc); err != nil { slog.Warn("discover vods", slog.Any("err", err), slog.String("component","vod_process")); return err }
	// Queue depth (unprocessed VODs)
	var queueDepth int
	_ = dbc.QueryRowContext(ctx, `SELECT COUNT(1) FROM vods WHERE COALESCE(processed,false)=false`).Scan(&queueDepth)
	slog.Debug("processing cycle queue depth", slog.Int("queue_depth", queueDepth), slog.String("component","vod_process"))
	telemetry.SetQueueDepth(queueDepth)
	maxAttempts := 5
	if s := os.Getenv("DOWNLOAD_MAX_ATTEMPTS"); s != "" { if n, err := strconv.Atoi(s); err == nil && n > 0 { maxAttempts = n } }
	cooldown := 600 * time.Second
	if s := os.Getenv("PROCESSING_RETRY_COOLDOWN"); s != "" { if d, err := time.ParseDuration(s); err == nil && d > 0 { cooldown = d } }
	row := dbc.QueryRow(`SELECT twitch_vod_id, title, date FROM vods
		WHERE COALESCE(processed,false)=false AND (
			processing_error IS NULL OR processing_error='' OR (download_retries < $1 AND EXTRACT(EPOCH FROM (NOW() - COALESCE(updated_at, created_at))) >= $2)
		)
		ORDER BY priority DESC, date ASC LIMIT 1`, maxAttempts, int(cooldown.Seconds()))
	var id, title string
	var date time.Time
	if err := row.Scan(&id, &title, &date); err != nil {
		if err == sql.ErrNoRows { slog.Debug("no vods ready for processing", slog.String("component","vod_process")) ; return nil }
		return err
	}
	logger := slog.Default().With(slog.String("vod_id", id), slog.String("component", "vod_process"))
	if corr := ctx.Value(struct{ string }{"corr"}); corr != nil { logger = logger.With(slog.Any("corr", corr)) }
	logger.Info("processing candidate selected", slog.String("title", title), slog.Time("date", date), slog.Int("queue_depth", queueDepth))
	// Metrics
	telemetry.ProcessingCycles.Inc()
	procStart := time.Now()
	dlStart := time.Now()
	filePath, err := downloader.Download(ctx, dbc, id, dataDir)
	if err != nil {
		lower := strings.ToLower(err.Error())
		// Expected/auth-required: skip retries and do not trip circuit
		if strings.Contains(lower, "subscriber-only") || strings.Contains(lower, "must be logged into") || strings.Contains(lower, "login required") {
			logger.Warn("skipping vod: auth required (subscriber-only)")
			// Mark non-retriable by setting retries to maxAttempts
			_, _ = dbc.Exec(`UPDATE vods SET processing_error=$1, download_retries=$2, updated_at=NOW() WHERE twitch_vod_id=$3`, "auth-required: subscriber-only", maxAttempts, id)
			return nil
		}
		// Otherwise count as a failure and trip the circuit
		logger.Error("download failed", slog.Any("err", err), slog.Duration("download_duration", time.Since(dlStart)), slog.Int("queue_depth", queueDepth))
		telemetry.DownloadsFailed.Inc()
		_, _ = dbc.Exec(`UPDATE vods SET processing_error=$1, updated_at=NOW() WHERE twitch_vod_id=$2`, err.Error(), id)
		updateCircuitOnFailure(ctx, dbc)
		telemetry.UpdateCircuitGauge(true)
		return nil
	}
	dlDur := time.Since(dlStart)
	telemetry.DownloadsSucceeded.Inc()
	telemetry.DownloadDuration.Observe(dlDur.Seconds())
	logger.Info("download complete", slog.String("path", filePath), slog.Duration("download_duration", dlDur))
	resetCircuit(ctx, dbc)
	_, _ = dbc.Exec(`UPDATE vods SET downloaded_path=$1, updated_at=NOW() WHERE twitch_vod_id=$2`, filePath, id)
	upStart := time.Now()
	ytURL, err := uploader.Upload(ctx, filePath, title, date)
	if err != nil {
		logger.Error("upload failed", slog.Any("err", err), slog.Duration("download_duration", dlDur), slog.Duration("upload_duration", time.Since(upStart)))
	_, _ = dbc.Exec(`UPDATE vods SET processing_error=$1, updated_at=NOW() WHERE twitch_vod_id=$2`, err.Error(), id)
		telemetry.UploadsFailed.Inc()
		return nil
	}
	// Record YouTube URL and mark processed now; we may adjust downloaded_path below if we delete.
	_, _ = dbc.Exec(`UPDATE vods SET youtube_url=$1, processed=TRUE, updated_at=NOW() WHERE twitch_vod_id=$2`, ytURL, id)
	// Retention and optimization policy
	// BACKFILL_AUTOCLEAN: if not "0", delete local file for older VODs (back catalog)
	// RETAIN_KEEP_NEWER_THAN_DAYS: window to consider a VOD "new" (default 7)
	keepDays := 7
	if s := os.Getenv("RETAIN_KEEP_NEWER_THAN_DAYS"); s != "" { if n, err := strconv.Atoi(s); err == nil && n >= 0 { keepDays = n } }
	backfillAutoclean := os.Getenv("BACKFILL_AUTOCLEAN") != "0" // default on
	cutoff := time.Now().Add(-time.Duration(keepDays) * 24 * time.Hour)
	isBackfill := date.Before(cutoff)
	if isBackfill && backfillAutoclean {
		if filePath != "" {
			if err := os.Remove(filePath); err != nil {
				logger.Warn("autoclean delete failed", slog.String("path", filePath), slog.Any("err", err))
			} else {
				logger.Info("autoclean removed local file", slog.String("path", filePath))
				_, _ = dbc.Exec(`UPDATE vods SET downloaded_path=NULL, updated_at=NOW() WHERE twitch_vod_id=$1`, id)
			}
		}
	} else if !isBackfill && filePath != "" {
		// Optional re-encode for efficient storage of new VODs
		mode := strings.ToLower(os.Getenv("NEW_VOD_STORE_MODE")) // original|hevc|av1|lossless-hevc
		if mode == "" { mode = "original" }
		if mode != "original" {
			preset := os.Getenv("NEW_VOD_PRESET")
			if preset == "" { preset = "medium" }
			crf := 23
			if s := os.Getenv("NEW_VOD_CRF"); s != "" { if n, err := strconv.Atoi(s); err == nil && n >= 0 { crf = n } }
			audio := strings.ToLower(os.Getenv("NEW_VOD_AUDIO")) // copy|aac128
			if audio == "" { audio = "copy" }
			args := []string{"-y", "-i", filePath}
			switch mode {
			case "hevc":
				args = append(args, "-c:v", "libx265", "-preset", preset, "-crf", fmt.Sprintf("%d", crf), "-tag:v", "hvc1")
			case "lossless-hevc":
				args = append(args, "-c:v", "libx265", "-preset", preset, "-x265-params", "lossless=1", "-tag:v", "hvc1")
			case "av1":
				// Use SVT-AV1 if available; otherwise this will fail and we log it.
				// Typical CRF around 30-40 for reasonable size/quality.
				if crf == 23 { crf = 32 }
				if preset == "medium" { preset = "6" } // SVT-AV1 preset scale (0 fastest .. 13 slowest)
				args = append(args, "-c:v", "libsvtav1", "-preset", preset, "-crf", fmt.Sprintf("%d", crf))
			default:
				// unknown -> skip
				args = nil
			}
			if len(args) > 0 {
				if audio == "aac128" {
					args = append(args, "-c:a", "aac", "-b:a", "128k")
				} else {
					args = append(args, "-c:a", "copy")
				}
				tmp := filePath + ".transcode.tmp.mp4"
				args = append(args, tmp)
				cmd := exec.CommandContext(ctx, "ffmpeg", args...)
				if out, err := cmd.CombinedOutput(); err != nil {
					logger.Warn("transcode failed", slog.Any("err", err), slog.String("out", string(out)), slog.Any("args", args))
					_ = os.Remove(tmp)
				} else {
					if err := os.Rename(tmp, filePath); err != nil {
						logger.Warn("transcode rename failed", slog.Any("err", err))
						_ = os.Remove(tmp)
					} else {
						logger.Info("stored new VOD with recompression", slog.String("codec", mode), slog.String("path", filePath))
					}
				}
			}
		}
	}
	upDur := time.Since(upStart)
	totalDur := time.Since(procStart)
	telemetry.UploadsSucceeded.Inc()
	telemetry.UploadDuration.Observe(upDur.Seconds())
	telemetry.TotalProcessDuration.Observe(totalDur.Seconds())
	updateMovingAvg(ctx, dbc, "avg_download_ms", float64(dlDur.Milliseconds()))
	updateMovingAvg(ctx, dbc, "avg_upload_ms", float64(upDur.Milliseconds()))
	updateMovingAvg(ctx, dbc, "avg_total_ms", float64(totalDur.Milliseconds()))
	logger.Info("processed vod", slog.String("youtube_url", ytURL), slog.Duration("download_duration", dlDur), slog.Duration("upload_duration", upDur), slog.Duration("total_duration", totalDur), slog.Int("queue_depth", queueDepth-1))
	telemetry.SetQueueDepth(queueDepth - 1)
	telemetry.UpdateCircuitGauge(false)
	return nil
}

// updateMovingAvg maintains a simple exponential moving average (EMA) stored in kv.
// alpha = 0.2 (new contributes 20%). Values stored as integer milliseconds.
func updateMovingAvg(ctx context.Context, db *sql.DB, key string, newVal float64) {
	const alpha = 0.2
	var existing string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=$1`, key).Scan(&existing)
	if existing == "" {
		_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ($1,$2,NOW())
			ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, key, fmt.Sprintf("%.0f", newVal))
		return
	}
	var old float64
	if v, err := strconv.ParseFloat(existing, 64); err == nil { old = v }
	ema := alpha*newVal + (1-alpha)*old
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ($1,$2,NOW())
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, key, fmt.Sprintf("%.0f", ema))
}

// uploadToYouTube uploads the given video file using stored OAuth token.
func uploadToYouTube(ctx context.Context, path, title string, date time.Time) (string, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" { dsn = "postgres://vod:vod@postgres:5432/vod?sslmode=disable" }
	adb, err := sql.Open("pgx", dsn)
	if err != nil { return "", err }
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
