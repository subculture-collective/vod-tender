package vod

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

func StartVODProcessingJob(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("vod processing job stopped")
			return
		case <-ticker.C:
			ProcessUnprocessedVODs(db)
		}
	}
}

func ProcessUnprocessedVODs(db *sql.DB) {
	slog.Info("processing unprocessed VODs (not yet implemented)")
}
