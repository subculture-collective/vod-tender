package vod

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RetentionPolicy defines how to determine which VODs to clean up.
type RetentionPolicy struct {
	// KeepLastNDays: VODs older than this many days are eligible for cleanup (0 = disabled)
	KeepLastNDays int
	// KeepLastNVODs: Keep only the N most recent VODs (0 = disabled)
	KeepLastNVODs int
	// DryRun: When true, log actions but don't delete files or update DB
	DryRun bool
	// Interval: How often to run the cleanup job
	Interval time.Duration
}

// LoadRetentionPolicy loads retention policy configuration from environment variables.
func LoadRetentionPolicy() RetentionPolicy {
	policy := RetentionPolicy{
		Interval: 6 * time.Hour, // Default to run every 6 hours
	}

	// Load keep days policy
	if s := os.Getenv("RETENTION_KEEP_DAYS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			policy.KeepLastNDays = n
		}
	}

	// Load keep count policy
	if s := os.Getenv("RETENTION_KEEP_COUNT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			policy.KeepLastNVODs = n
		}
	}

	// Load dry-run mode
	if os.Getenv("RETENTION_DRY_RUN") == "1" {
		policy.DryRun = true
	}

	// Load interval
	if s := os.Getenv("RETENTION_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			policy.Interval = d
		}
	}

	return policy
}

// StartRetentionJob runs a background job that periodically cleans up old VOD files
// according to the configured retention policy.
func StartRetentionJob(ctx context.Context, dbc *sql.DB, channel string) {
	policy := LoadRetentionPolicy()

	// Skip if no retention policy is configured
	if policy.KeepLastNDays == 0 && policy.KeepLastNVODs == 0 {
		slog.Info("retention job disabled (no policy configured)", slog.String("channel", channel))
		return
	}

	slog.Info("retention job starting",
		slog.String("channel", channel),
		slog.Int("keep_days", policy.KeepLastNDays),
		slog.Int("keep_count", policy.KeepLastNVODs),
		slog.Bool("dry_run", policy.DryRun),
		slog.Duration("interval", policy.Interval))

	// Run immediately on start
	if err := runRetentionCleanup(ctx, dbc, channel, policy); err != nil {
		slog.Warn("retention cleanup failed", slog.Any("err", err), slog.String("channel", channel))
	}

	ticker := time.NewTicker(policy.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("retention job stopped", slog.String("channel", channel))
			return
		case <-ticker.C:
			if err := runRetentionCleanup(ctx, dbc, channel, policy); err != nil {
				slog.Warn("retention cleanup failed", slog.Any("err", err), slog.String("channel", channel))
			}
		}
	}
}

// runRetentionCleanup performs a single retention cleanup cycle.
func runRetentionCleanup(ctx context.Context, dbc *sql.DB, channel string, policy RetentionPolicy) error {
	logger := slog.Default().With(
		slog.String("component", "retention_cleanup"),
		slog.String("channel", channel),
		slog.Bool("dry_run", policy.DryRun),
	)

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	// Build list of VOD IDs that should be retained
	retainedIDs := make(map[string]struct{})

	// Policy 1: Keep VODs newer than N days
	if policy.KeepLastNDays > 0 {
		cutoff := time.Now().Add(-time.Duration(policy.KeepLastNDays) * 24 * time.Hour)
		rows, err := dbc.QueryContext(ctx,
			`SELECT twitch_vod_id FROM vods WHERE channel=$1 AND date >= $2`,
			channel, cutoff)
		if err != nil {
			return fmt.Errorf("query recent vods: %w", err)
		}
		defer func() {
			if err := rows.Close(); err != nil {
				slog.Warn("failed to close rows", slog.Any("err", err))
			}
		}()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				retainedIDs[id] = struct{}{}
			}
		}
		logger.Debug("identified vods to retain by date", slog.Int("count", len(retainedIDs)))
	}

	// Policy 2: Keep last N VODs (most recent by date)
	if policy.KeepLastNVODs > 0 {
		rows, err := dbc.QueryContext(ctx,
			`SELECT twitch_vod_id FROM vods WHERE channel=$1 ORDER BY date DESC LIMIT $2`,
			channel, policy.KeepLastNVODs)
		if err != nil {
			return fmt.Errorf("query last n vods: %w", err)
		}
		defer func() {
			if err := rows.Close(); err != nil {
				slog.Warn("failed to close rows", slog.Any("err", err))
			}
		}()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				retainedIDs[id] = struct{}{}
			}
		}
		logger.Debug("identified vods to retain by count", slog.Int("retained_count", len(retainedIDs)))
	}

	// Safety check: Never delete VODs that are currently being processed or uploaded
	// Identify by checking for null youtube_url but with a downloaded_path or recent processing activity
	rows, err := dbc.QueryContext(ctx, `
		SELECT twitch_vod_id FROM vods 
		WHERE channel=$1 
		AND (
			-- Currently being processed
			(processed = false AND downloaded_path IS NOT NULL)
			-- Recently updated (within last hour, might be uploading)
			OR (updated_at > NOW() - INTERVAL '1 hour' AND youtube_url IS NULL AND downloaded_path IS NOT NULL)
			-- Has active download state
			OR download_state IN ('downloading', 'processing')
		)
	`, channel)
	if err != nil {
		return fmt.Errorf("query active vods: %w", err)
	}
	activeIDs := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			activeIDs[id] = struct{}{}
			retainedIDs[id] = struct{}{} // Also retain active items
		}
	}
	if err := rows.Close(); err != nil {
		slog.Warn("failed to close rows", slog.Any("err", err))
	}
	logger.Debug("identified active vods to protect", slog.Int("count", len(activeIDs)))

	// Find VODs eligible for cleanup (have downloaded files but not in retained set)
	rows, err = dbc.QueryContext(ctx, `
		SELECT twitch_vod_id, downloaded_path, date, title 
		FROM vods 
		WHERE channel=$1 AND downloaded_path IS NOT NULL AND downloaded_path != ''
		ORDER BY date ASC
	`, channel)
	if err != nil {
		return fmt.Errorf("query vods with files: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("failed to close rows", slog.Any("err", err))
		}
	}()

	var cleaned, skipped, errors int
	var bytesFreed int64

	for rows.Next() {
		var id, path, title string
		var date time.Time
		if err := rows.Scan(&id, &path, &date, &title); err != nil {
			logger.Warn("failed to scan vod row", slog.Any("err", err))
			continue
		}

		// Skip if this VOD should be retained
		if _, retained := retainedIDs[id]; retained {
			skipped++
			continue
		}

		// Double-check safety: skip if in active set (redundant but safe)
		if _, active := activeIDs[id]; active {
			skipped++
			logger.Debug("skipping active vod", slog.String("vod_id", id))
			continue
		}

		// Check if file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// File already gone, just clear the DB field
			if !policy.DryRun {
				_, err := dbc.ExecContext(ctx, `UPDATE vods SET downloaded_path=NULL WHERE twitch_vod_id=$1`, id)
				if err != nil {
					logger.Warn("failed to clear db reference for missing file", slog.String("vod_id", id), slog.Any("err", err))
				}
			}
			logger.Debug("file already missing, clearing db reference", slog.String("path", path), slog.String("vod_id", id))
			continue
		} else if err != nil {
			logger.Warn("failed to stat file", slog.String("path", path), slog.Any("err", err))
			errors++
			continue
		}

		// Get file size for reporting
		fileInfo, err := os.Stat(path)
		if err == nil {
			bytesFreed += fileInfo.Size()
		}

		if policy.DryRun {
			logger.Info("dry-run: would delete file",
				slog.String("path", path),
				slog.String("vod_id", id),
				slog.String("title", title),
				slog.Time("date", date),
				slog.Int64("size_bytes", fileInfo.Size()))
			cleaned++
		} else {
			// Delete the file
			if err := os.Remove(path); err != nil {
				logger.Warn("failed to delete file",
					slog.String("path", path),
					slog.String("vod_id", id),
					slog.Any("err", err))
				errors++
				continue
			}

			// Update database to clear the path
			_, err := dbc.ExecContext(ctx, `UPDATE vods SET downloaded_path=NULL, updated_at=NOW() WHERE twitch_vod_id=$1`, id)
			if err != nil {
				logger.Warn("failed to update db after deletion",
					slog.String("vod_id", id),
					slog.Any("err", err))
				errors++
				continue
			}

			logger.Info("deleted old vod file",
				slog.String("path", path),
				slog.String("vod_id", id),
				slog.String("title", title),
				slog.Time("date", date),
				slog.Int64("size_bytes", fileInfo.Size()))
			cleaned++
		}
	}

	// Log summary
	mode := "cleanup"
	if policy.DryRun {
		mode = "dry-run"
	}
	logger.Info("retention cleanup completed",
		slog.String("mode", mode),
		slog.Int("cleaned", cleaned),
		slog.Int("skipped", skipped),
		slog.Int("errors", errors),
		slog.Int64("bytes_freed", bytesFreed))

	return nil
}

// CleanupTempFiles removes stale temporary and partial files from the data directory.
// This is a separate concern from retention and runs as part of the processing job.
func CleanupTempFiles(dataDir string, maxAge time.Duration) {
	if maxAge <= 0 {
		return
	}

	now := time.Now()
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		slog.Warn("failed to read data dir for temp cleanup", slog.String("dir", dataDir), slog.Any("err", err))
		return
	}

	var removed, failed int
	for _, e := range entries {
		name := e.Name()
		// Only consider temp/partial files
		if !strings.HasSuffix(name, ".part") &&
			!strings.HasSuffix(name, ".tmp") &&
			!strings.Contains(name, ".transcode.tmp.") {
			continue
		}

		fi, err := e.Info()
		if err != nil {
			continue
		}

		if now.Sub(fi.ModTime()) > maxAge {
			path := filepath.Join(dataDir, name)
			if err := os.Remove(path); err == nil {
				removed++
				slog.Debug("removed stale temp file", slog.String("path", path), slog.Duration("age", now.Sub(fi.ModTime())))
			} else {
				failed++
				slog.Warn("failed to remove stale temp file", slog.String("path", path), slog.Any("err", err))
			}
		}
	}

	if removed > 0 || failed > 0 {
		slog.Info("temp file cleanup completed", slog.Int("removed", removed), slog.Int("failed", failed))
	}
}
