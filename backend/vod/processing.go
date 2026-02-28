package vod

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"

	"github.com/onnwee/vod-tender/backend/config"
	"github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/telemetry"
	youtubeapi "github.com/onnwee/vod-tender/backend/youtubeapi"
)

// Downloader abstracts video retrieval (for tests/mocks).
type Downloader interface {
	Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error)
}

// Uploader abstracts upload destination behavior.
type Uploader interface {
	Upload(ctx context.Context, dbc *sql.DB, path, title string, date time.Time) (string, error)
}

// default implementations wrap existing functions.
type ytDLPDownloader struct{}

func (ytDLPDownloader) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) {
	return downloadVOD(ctx, dbc, id, dataDir)
}

type youtubeUploader struct{}

func (youtubeUploader) Upload(ctx context.Context, dbc *sql.DB, path, title string, date time.Time) (string, error) {
	return uploadToYouTube(ctx, dbc, path, title, date)
}

// vodCustomDescKey is an unexported type used as a context key for custom VOD descriptions.
// Using a named type prevents collisions with other context keys.
type vodCustomDescKey struct{}

// context keys for upload metadata
type vodIDCtxKey struct{}
type vodChannelCtxKey struct{}

// configurable for tests
var (
	downloader Downloader = ytDLPDownloader{}
	uploader   Uploader   = youtubeUploader{}
)

// StartVODProcessingJob runs a loop that picks the next unprocessed VOD and processes it.
// It is safe to run a single instance per process; for multiple workers add distributed coordination.
// The channel parameter filters VODs to process for a specific Twitch channel.
func StartVODProcessingJob(ctx context.Context, dbc *sql.DB, channel string) {
	interval := 1 * time.Minute
	if s := os.Getenv("VOD_PROCESS_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			interval = d
		}
	}
	slog.Info("vod processing job starting", slog.Duration("interval", interval), slog.String("channel", channel))
	// Kick an immediate run so we don't wait a full interval after boot.
	if err := processOnce(ctx, dbc, channel); err != nil {
		slog.Warn("process once", slog.Any("err", err))
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("vod processing job stopped", slog.String("channel", channel))
			return
		case <-ticker.C:
			if err := processOnce(ctx, dbc, channel); err != nil {
				slog.Warn("process once", slog.Any("err", err))
			}
		}
	}
}

// processOnce selects a single pending VOD and processes it.
// It implements a simple circuit breaker to avoid hot-looping on systemic failures.
func processOnce(ctx context.Context, dbc *sql.DB, channel string) error {
	ctx, span := telemetry.StartSpan(ctx, "vod-processing", "processOnce")
	defer span.End()

	_, _ = dbc.ExecContext(ctx, `INSERT INTO kv (channel,key,value,updated_at) VALUES ($1,'job_vod_process_last', to_char(NOW() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), NOW())
		ON CONFLICT(channel,key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, channel)
	var state, until string
	_ = dbc.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel=$1 AND key='circuit_state'`, channel).Scan(&state)
	if state == "open" {
		_ = dbc.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel=$1 AND key='circuit_open_until'`, channel).Scan(&until)
		if until != "" {
			if t, err := time.Parse(time.RFC3339, until); err == nil {
				if time.Now().Before(t) {
					slog.Debug("circuit open; skipping processing cycle", slog.String("until", until), slog.String("channel", channel))
					span.SetAttributes(attribute.String("circuit.state", "open"))
					telemetry.SetCircuitState("open")
					return nil
				}
				_, _ = dbc.ExecContext(ctx, `INSERT INTO kv (channel,key,value,updated_at) VALUES ($1,'circuit_state','half-open',CURRENT_TIMESTAMP)
					ON CONFLICT(channel,key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, channel)
				slog.Info("circuit transitioning to half-open", slog.String("channel", channel))
				span.SetAttributes(attribute.String("circuit.state", "half-open"))
				telemetry.SetCircuitState("half-open")
				telemetry.RecordCircuitStateChange("open", "half-open")
			}
		}
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}
	// Best-effort cleanup: prune stale partial/tmp files to keep /data small
	// Controlled via DATA_CLEANUP_MAX_AGE (default 24h). Set to 0 to disable.
	maxAge := 24 * time.Hour
	if s := os.Getenv("DATA_CLEANUP_MAX_AGE"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			maxAge = d
		}
	}
	if maxAge > 0 {
		now := time.Now()
		if entries, err := os.ReadDir(dataDir); err == nil {
			for _, e := range entries {
				name := e.Name()
				if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".tmp") || strings.Contains(name, ".transcode.tmp.mp4") {
					if fi, err := e.Info(); err == nil {
						if now.Sub(fi.ModTime()) > maxAge {
							_ = os.Remove(filepath.Join(dataDir, name))
						}
					}
				}
			}
		}
	}

	// Optional orphan sweeper: prune stale full files not referenced by DB and older than RETAIN_KEEP_NEWER_THAN_DAYS.
	// This helps clean up any leftovers from crashes or manual copies.
	if keepDaysStr := os.Getenv("RETAIN_KEEP_NEWER_THAN_DAYS"); keepDaysStr != "" {
		if keepDays, err := strconv.Atoi(keepDaysStr); err == nil && keepDays >= 0 {
			cutoff := time.Now().Add(-time.Duration(keepDays) * 24 * time.Hour)
			// Build a set of active paths from DB
			active := map[string]struct{}{}
			rows, err := dbc.QueryContext(ctx, `SELECT downloaded_path FROM vods WHERE channel=$1 AND downloaded_path IS NOT NULL`, channel)
			if err == nil {
				defer func() {
					if err := rows.Close(); err != nil {
						slog.Warn("failed to close rows", slog.Any("err", err))
					}
				}()
				for rows.Next() {
					var p string
					if err := rows.Scan(&p); err == nil && p != "" {
						active[p] = struct{}{}
					}
				}
			}
			if entries, err := os.ReadDir(dataDir); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						continue
					}
					// Only consider video-like files for sweeping
					name := e.Name()
					nameLower := strings.ToLower(name)
					if strings.HasSuffix(nameLower, ".mp4") || strings.HasSuffix(nameLower, ".mkv") || strings.HasSuffix(nameLower, ".webm") {
						path := filepath.Join(dataDir, name)
						if _, ok := active[path]; ok {
							continue
						}
						if fi, err := e.Info(); err == nil {
							if fi.ModTime().Before(cutoff) {
								if err := os.Remove(path); err == nil {
									slog.Info("sweeper removed orphaned file", slog.String("path", path))
								} else {
									slog.Warn("sweeper failed to remove orphaned file", slog.String("path", path), slog.Any("err", err))
								}
							}
						}
					}
				}
			}
		}
	}
	if err := DiscoverAndUpsert(ctx, dbc, channel); err != nil {
		slog.Warn("discover vods", slog.Any("err", err), slog.String("component", "vod_process"), slog.String("channel", channel))
		return err
	}
	// Queue depth (unprocessed VODs)
	var queueDepth int
	_ = dbc.QueryRowContext(ctx, `SELECT COUNT(1) FROM vods WHERE channel=$1 AND COALESCE(processed,false)=false`, channel).Scan(&queueDepth)
	slog.Debug("processing cycle queue depth", slog.Int("queue_depth", queueDepth), slog.String("component", "vod_process"), slog.String("channel", channel))
	telemetry.SetQueueDepth(queueDepth)
	// Global upload throttling: hard cap uploads per 24h window (all VODs).
	uploadDailyLimit := 10
	if s := os.Getenv("UPLOAD_DAILY_LIMIT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			uploadDailyLimit = n
		}
	}
	var uploaded24 int
	_ = dbc.QueryRowContext(ctx, `SELECT COUNT(1) FROM vods WHERE channel=$1 AND youtube_url IS NOT NULL AND updated_at > (NOW() - INTERVAL '24 hours')`, channel).Scan(&uploaded24)
	if uploaded24 >= uploadDailyLimit {
		slog.Info("global upload throttled for 24h window", slog.Int("uploaded24h", uploaded24), slog.Int("limit", uploadDailyLimit), slog.String("channel", channel))
		return nil
	}

	// Backfill upload throttling: limit back-catalog uploads per 24h window.
	// Define back-catalog as VODs older than RETAIN_KEEP_NEWER_THAN_DAYS (default 7 days).
	backfillDays := 7
	if s := os.Getenv("RETAIN_KEEP_NEWER_THAN_DAYS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			backfillDays = n
		}
	}
	backfillCutoff := time.Now().Add(-time.Duration(backfillDays) * 24 * time.Hour)
	dailyLimit := 10
	if s := os.Getenv("BACKFILL_UPLOAD_DAILY_LIMIT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			dailyLimit = n
		}
	}
	// Count successful uploads of back-catalog in past 24h
	var backfillUploaded24 int
	_ = dbc.QueryRowContext(ctx, `SELECT COUNT(1) FROM vods WHERE channel=$1 AND youtube_url IS NOT NULL AND date < $2 AND updated_at > (NOW() - INTERVAL '24 hours')`, channel, backfillCutoff).Scan(&backfillUploaded24)
	backfillThrottled := backfillUploaded24 >= dailyLimit
	maxAttempts := 5
	if s := os.Getenv("DOWNLOAD_MAX_ATTEMPTS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			maxAttempts = n
		}
	}
	cooldown := 600 * time.Second
	if s := os.Getenv("PROCESSING_RETRY_COOLDOWN"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			cooldown = d
		}
	}
	// Select a small batch of candidates and pick the first eligible.
	rows, err := dbc.QueryContext(ctx, `SELECT twitch_vod_id, title, date, COALESCE(skip_upload,FALSE) FROM vods
		WHERE channel=$1 AND COALESCE(processed,false)=false AND (
			processing_error IS NULL OR processing_error='' OR (download_retries < $2 AND EXTRACT(EPOCH FROM (NOW() - COALESCE(updated_at, created_at))) >= $3)
		)
		ORDER BY priority DESC, date ASC LIMIT 20`, channel, maxAttempts, int(cooldown.Seconds()))
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("failed to close rows", slog.Any("err", err))
		}
	}()
	var id, title string
	var date time.Time
	var skipUpload bool
	picked := false
	for rows.Next() {
		var cid, ctitle string
		var cdate time.Time
		var cskipUpload bool
		if err := rows.Scan(&cid, &ctitle, &cdate, &cskipUpload); err != nil {
			return err
		}
		isBackfill := cdate.Before(backfillCutoff)
		if backfillThrottled && isBackfill {
			// Skip back-catalog while throttled; continue searching for a newer (non-backfill) item.
			continue
		}
		id, title, date, skipUpload = cid, ctitle, cdate, cskipUpload
		picked = true
		break
	}
	if !picked {
		if backfillThrottled {
			slog.Info("backfill upload throttled for 24h window; no eligible non-backfill items", slog.Int("uploaded24h", backfillUploaded24), slog.Int("limit", dailyLimit))
		} else {
			slog.Debug("no vods ready for processing", slog.String("component", "vod_process"))
		}
		return nil
	}

	// Add span attributes for selected VOD
	span.SetAttributes(
		attribute.String("vod.id", id),
		attribute.String("vod.title", title),
		attribute.String("vod.date", date.Format(time.RFC3339)),
		attribute.Int("queue_depth", queueDepth),
	)

	logger := slog.Default().With(slog.String("vod_id", id), slog.String("component", "vod_process"))
	if corr := ctx.Value(struct{ string }{"corr"}); corr != nil {
		logger = logger.With(slog.Any("corr", corr))
	}
	logger.Info("processing candidate selected", slog.String("title", title), slog.Time("date", date), slog.Int("queue_depth", queueDepth))

	// Metrics
	telemetry.ProcessingCycles.Inc()
	procStart := time.Now()

	// Acquire download slot (blocks if max concurrent downloads reached)
	if logger.Enabled(ctx, slog.LevelDebug) {
		logger.Debug("waiting for download slot",
			slog.Int("active_downloads", GetActiveDownloads()),
			slog.Int("max_concurrent", GetMaxConcurrentDownloads()),
		)
	}
	if !acquireDownloadSlot(ctx) {
		// Context canceled while waiting for slot
		logger.Info("download canceled while waiting for slot")
		return nil
	}
	defer releaseDownloadSlot()
	logger.Debug("download slot acquired", slog.Int("active_downloads", GetActiveDownloads()))

	// Download with span
	dlStart := time.Now()
	ctx, downloadSpan := telemetry.StartSpan(ctx, "vod-processing", "download",
		attribute.String("vod.id", id),
		attribute.String("vod.title", title),
	)
	filePath, err := downloader.Download(ctx, dbc, id, dataDir)
	dlDur := time.Since(dlStart)
	downloadSpan.SetAttributes(attribute.Int64("download.duration_ms", dlDur.Milliseconds()))

	if err != nil {
		telemetry.RecordError(downloadSpan, err)
		downloadSpan.End()

		// Check if download was canceled (user-initiated or timeout)
		if ctx.Err() != nil {
			logger.Info("download canceled", slog.Any("reason", ctx.Err()))
			// Don't treat cancellation as a failure or trip circuit breaker
			return nil
		}

		lower := strings.ToLower(err.Error())
		// Expected/auth-required: skip retries and do not trip circuit
		if strings.Contains(lower, "subscriber-only") || strings.Contains(lower, "must be logged into") || strings.Contains(lower, "login required") {
			logger.Warn("skipping vod: auth required (subscriber-only)")
			// Mark non-retriable by setting retries to maxAttempts
			_, _ = dbc.ExecContext(ctx, `UPDATE vods SET processing_error=$1, download_retries=$2, updated_at=NOW() WHERE twitch_vod_id=$3`, "auth-required: subscriber-only", maxAttempts, id)
			return nil
		}
		// Otherwise count as a failure and trip the circuit
		logger.Error("download failed", slog.Any("err", err), slog.Duration("download_duration", dlDur), slog.Int("queue_depth", queueDepth))
		telemetry.DownloadsFailed.Inc()
		_, _ = dbc.ExecContext(ctx, `UPDATE vods SET processing_error=$1, updated_at=NOW() WHERE twitch_vod_id=$2`, err.Error(), id)
		updateCircuitOnFailure(ctx, dbc, channel)
		telemetry.UpdateCircuitGauge(true)
		return nil
	}

	telemetry.SetSpanSuccess(downloadSpan)
	downloadSpan.SetAttributes(attribute.String("download.path", filePath))
	downloadSpan.End()

	telemetry.DownloadsSucceeded.Inc()
	telemetry.DownloadDuration.Observe(dlDur.Seconds())
	logger.Info("download complete", slog.String("path", filePath), slog.Duration("download_duration", dlDur))
	resetCircuit(ctx, dbc, channel)
	_, _ = dbc.ExecContext(ctx, `UPDATE vods SET downloaded_path=$1, updated_at=NOW() WHERE twitch_vod_id=$2`, filePath, id)
	// Upload policy guardrails + idempotency checks.
	var preYT string
	_ = dbc.QueryRowContext(ctx, `SELECT COALESCE(youtube_url,'' ) FROM vods WHERE twitch_vod_id=$1`, id).Scan(&preYT)
	uplCfg, _ := config.Load()
	uploadOwnershipValid := uplCfg.YouTubeUploadOwnership == "self" || uplCfg.YouTubeUploadOwnership == "authorized"
	var ytURL string
	var upDur time.Duration
	if preYT != "" {
		ytURL = preYT
		slog.Info("skipping upload; youtube_url already present", slog.String("youtube_url", ytURL))
		// Ensure processed is marked; we'll still perform post-success cleanup below.
		_, _ = dbc.ExecContext(ctx, `UPDATE vods SET processed=TRUE, processing_error=NULL, updated_at=NOW() WHERE twitch_vod_id=$1`, id)
	} else if skipUpload {
		logger.Info("skipping upload; skip_upload=true for vod")
		_, _ = dbc.ExecContext(ctx, `UPDATE vods SET processed=TRUE, processing_error=NULL, updated_at=NOW() WHERE twitch_vod_id=$1`, id)
	} else if !uplCfg.YouTubeUploadEnabled {
		logger.Info("skipping upload; YOUTUBE_UPLOAD_ENABLED is not set")
		_, _ = dbc.ExecContext(ctx, `UPDATE vods SET processed=TRUE, processing_error=NULL, updated_at=NOW() WHERE twitch_vod_id=$1`, id)
	} else if !uploadOwnershipValid {
		logger.Warn("skipping upload; YOUTUBE_UPLOAD_OWNERSHIP must be self|authorized when uploads are enabled", slog.String("ownership", uplCfg.YouTubeUploadOwnership))
		_, _ = dbc.ExecContext(ctx, `UPDATE vods SET processed=TRUE, processing_error=NULL, updated_at=NOW() WHERE twitch_vod_id=$1`, id)
	} else {
		// Retry loop with exponential backoff + jitter for uploads
		maxUp := 5
		if s := os.Getenv("UPLOAD_MAX_ATTEMPTS"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				maxUp = n
			}
		}
		base := 2 * time.Second
		if s := os.Getenv("UPLOAD_BACKOFF_BASE"); s != "" {
			if d, err := time.ParseDuration(s); err == nil && d > 0 {
				base = d
			}
		}
		var lastErr error
		// Load any custom description set by user
		var customDesc string
		_ = dbc.QueryRowContext(ctx, `SELECT COALESCE(description,'') FROM vods WHERE twitch_vod_id=$1`, id).Scan(&customDesc)

		// Start upload span
		uploadCtx, uploadSpan := telemetry.StartSpan(ctx, "vod-processing", "upload",
			attribute.String("vod.id", id),
			attribute.String("vod.title", title),
			attribute.String("upload.path", filePath),
		)
		uploadCtx = context.WithValue(uploadCtx, vodIDCtxKey{}, id)
		uploadCtx = context.WithValue(uploadCtx, vodChannelCtxKey{}, channel)

		for attempt := 0; attempt < maxUp; attempt++ {
			if attempt > 0 {
				backoff := base * time.Duration(1<<attempt)
				//nolint:gosec // G404: math/rand is sufficient for exponential backoff jitter, not used for security
				jitter := time.Duration(rand.Int63n(int64(base)))
				backoff += jitter
				logger.Warn("retrying upload", slog.Int("attempt", attempt), slog.Int("max", maxUp), slog.Duration("backoff", backoff))
				time.Sleep(backoff)
			}
			upStart := time.Now()
			// Temporarily override description if custom provided via context key
			if customDesc != "" {
				uploadCtx = context.WithValue(uploadCtx, vodCustomDescKey{}, customDesc)
			}
			url, err := uploader.Upload(uploadCtx, dbc, filePath, title, date)
			if err == nil {
				upDur = time.Since(upStart)
				ytURL = url
				break
			}
			lastErr = err
			// Non-retriable: invalid title
			el := strings.ToLower(err.Error())
			if strings.Contains(el, "invalidtitle") || strings.Contains(el, "invalid or empty video title") {
				logger.Error("non-retriable upload error: invalid title", slog.Any("err", err))
				break
			}
			// If context canceled, abort early
			if uploadCtx.Err() != nil {
				break
			}
		}

		uploadSpan.SetAttributes(attribute.Int64("upload.duration_ms", upDur.Milliseconds()))

		if ytURL == "" {
			// Exhausted attempts; persist error and increment retries so global cooldown/limit logic applies
			logger.Error("upload exhausted retries", slog.Any("err", lastErr))
			telemetry.RecordError(uploadSpan, lastErr)
			uploadSpan.End()

			_, _ = dbc.ExecContext(ctx, `UPDATE vods SET processing_error=$1, download_retries = COALESCE(download_retries,0)+1, updated_at=NOW() WHERE twitch_vod_id=$2`,
				fmt.Sprintf("upload: %v", lastErr), id)
			telemetry.UploadsFailed.Inc()
			return nil
		}

		telemetry.SetSpanSuccess(uploadSpan)
		uploadSpan.SetAttributes(attribute.String("upload.youtube_url", ytURL))
		uploadSpan.End()

		// Record YouTube URL and mark processed now
		_, _ = dbc.ExecContext(ctx, `UPDATE vods SET youtube_url=$1, processed=TRUE, processing_error=NULL, updated_at=NOW() WHERE twitch_vod_id=$2`, ytURL, id)
	}

	// Clean up local file after successful upload (both backfill and new items)
	// Retention and optimization policy
	// BACKFILL_AUTOCLEAN: if not "0", delete local file for older VODs (back catalog) — legacy behavior
	// RETAIN_KEEP_NEWER_THAN_DAYS: window to consider a VOD "new" (default 7)
	keepDays := 7
	if s := os.Getenv("RETAIN_KEEP_NEWER_THAN_DAYS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			keepDays = n
		}
	}
	backfillAutoclean := os.Getenv("BACKFILL_AUTOCLEAN") != "0" // default on
	cutoff := time.Now().Add(-time.Duration(keepDays) * 24 * time.Hour)
	isBackfill := date.Before(cutoff)
	// New behavior: always try to remove local file after successful upload to save disk, while keeping legacy backfill flag for logs
	if ytURL != "" && filePath != "" {
		if err := os.Remove(filePath); err != nil {
			logger.Warn("delete local file failed", slog.String("path", filePath), slog.Any("err", err))
		} else {
			if isBackfill && backfillAutoclean {
				logger.Info("autoclean removed local file", slog.String("path", filePath))
			} else {
				logger.Info("removed local file after upload", slog.String("path", filePath))
			}
			_, _ = dbc.ExecContext(ctx, `UPDATE vods SET downloaded_path=NULL, updated_at=NOW() WHERE twitch_vod_id=$1`, id)
		}
	}
	// If we performed an upload in this run, we have upDur set; otherwise it may be zero for idempotent path
	totalDur := time.Since(procStart)
	if upDur > 0 {
		telemetry.UploadsSucceeded.Inc()
		telemetry.UploadDuration.Observe(upDur.Seconds())
		if telemetry.ProcessingStepDuration != nil {
			telemetry.ProcessingStepDuration.WithLabelValues("upload").Observe(upDur.Seconds())
		}
	}
	telemetry.TotalProcessDuration.Observe(totalDur.Seconds())
	if telemetry.ProcessingStepDuration != nil {
		telemetry.ProcessingStepDuration.WithLabelValues("download").Observe(dlDur.Seconds())
		telemetry.ProcessingStepDuration.WithLabelValues("total").Observe(totalDur.Seconds())
	}

	updateMovingAvg(ctx, dbc, channel, "avg_download_ms", float64(dlDur.Milliseconds()))
	if upDur > 0 {
		updateMovingAvg(ctx, dbc, channel, "avg_upload_ms", float64(upDur.Milliseconds()))
	}
	updateMovingAvg(ctx, dbc, channel, "avg_total_ms", float64(totalDur.Milliseconds()))

	// Set final span attributes
	span.SetAttributes(
		attribute.Int64("download.duration_ms", dlDur.Milliseconds()),
		attribute.Int64("upload.duration_ms", upDur.Milliseconds()),
		attribute.Int64("total.duration_ms", totalDur.Milliseconds()),
		attribute.String("youtube_url", ytURL),
	)
	telemetry.SetSpanSuccess(span)

	logger.Info("processed vod", slog.String("youtube_url", ytURL), slog.Duration("download_duration", dlDur), slog.Duration("upload_duration", upDur), slog.Duration("total_duration", totalDur), slog.Int("queue_depth", queueDepth-1))
	telemetry.SetQueueDepth(queueDepth - 1)
	telemetry.UpdateCircuitGauge(false)
	return nil
}

// updateMovingAvg maintains a simple exponential moving average (EMA) stored in kv.
// alpha = 0.2 (new contributes 20%). Values stored as integer milliseconds.
func updateMovingAvg(ctx context.Context, db *sql.DB, channel, key string, newVal float64) {
	const alpha = 0.2
	var existing string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel=$1 AND key=$2`, channel, key).Scan(&existing)
	if existing == "" {
		_, _ = db.ExecContext(ctx, `INSERT INTO kv (channel,key,value,updated_at) VALUES ($1,$2,$3,NOW())
			ON CONFLICT(channel,key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, channel, key, fmt.Sprintf("%.0f", newVal))
		return
	}
	var old float64
	if v, err := strconv.ParseFloat(existing, 64); err == nil {
		old = v
	}
	ema := alpha*newVal + (1-alpha)*old
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (channel,key,value,updated_at) VALUES ($1,$2,$3,NOW())
		ON CONFLICT(channel,key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, channel, key, fmt.Sprintf("%.0f", ema))
}

// uploadToYouTube uploads the given video file using stored OAuth token.
func uploadToYouTube(ctx context.Context, dbc *sql.DB, path, title string, date time.Time) (string, error) {
	ts := &db.TokenStoreAdapter{DB: dbc}
	cfg, _ := config.Load()
	yts := youtubeapi.New(cfg, ts)
	svc, err := yts.Client(ctx)
	if err != nil {
		return "", fmt.Errorf("youtube client: %w", err)
	}
	datePrefix := date.Format("2006-01-02")
	// Sanitize and validate title: non-empty, trimmed, max 100 chars, no control chars
	t := strings.TrimSpace(title)
	if t == "" {
		t = "Twitch VOD"
	}
	// Remove control characters
	clean := make([]rune, 0, len(t))
	for _, r := range t {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 {
			continue
		}
		clean = append(clean, r)
	}
	t = string(clean)
	finalTitle := fmt.Sprintf("%s %s", datePrefix, t)
	if len([]rune(finalTitle)) > 100 {
		runes := []rune(finalTitle)
		finalTitle = string(runes[:97]) + "..."
	}
	vodID := ""
	if v := ctx.Value(vodIDCtxKey{}); v != nil {
		if s, ok := v.(string); ok {
			vodID = strings.TrimSpace(s)
		}
	}
	channel := ""
	if v := ctx.Value(vodChannelCtxKey{}); v != nil {
		if s, ok := v.(string); ok {
			channel = strings.TrimSpace(s)
		}
	}

	metaLines := []string{fmt.Sprintf("Original stream date: %s", date.Format(time.RFC3339))}
	if channel != "" {
		metaLines = append(metaLines, fmt.Sprintf("Attribution: Original Twitch channel %q", channel))
	}
	if vodID != "" {
		metaLines = append(metaLines,
			fmt.Sprintf("Original Twitch VOD ID: %s", vodID),
			fmt.Sprintf("Original Twitch URL: https://www.twitch.tv/videos/%s", vodID),
		)
	}

	// Use custom description if provided in context (set by processOnce), while preserving attribution metadata.
	description := strings.Join(metaLines, "\n")
	if v := ctx.Value(vodCustomDescKey{}); v != nil {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			description = strings.TrimSpace(s) + "\n\n" + strings.Join(metaLines, "\n")
		}
	}
	return youtubeapi.UploadVideo(ctx, svc, path, finalTitle, description, "private")
}

