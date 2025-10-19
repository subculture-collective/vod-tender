package vod

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/onnwee/vod-tender/backend/twitchapi"
)

// BackfillMetadata fetches channel VOD list and updates rows that lack title or duration.
func BackfillMetadata(ctx context.Context, db *sql.DB) error {
	vods, err := FetchChannelVODs(ctx)
	if err != nil {
		return err
	}
	for _, v := range vods {
		_, _ = db.ExecContext(ctx, `UPDATE vods SET title=COALESCE(NULLIF(title,''), $1), date=$2, duration_seconds=CASE WHEN COALESCE(duration_seconds,0)=0 THEN $3 ELSE duration_seconds END, updated_at=NOW() WHERE twitch_vod_id=$4`, v.Title, v.Date, v.Duration, v.ID)
	}
	return nil
}

// FetchAllChannelVODs pages through the channel's archive VODs up to maxCount or maxAge.
func FetchAllChannelVODs(ctx context.Context, db *sql.DB, maxCount int, maxAge time.Duration) ([]VOD, error) {
	channel := os.Getenv("TWITCH_CHANNEL")
	if channel == "" {
		return nil, nil
	}
	client := helixClient()
	userID, err := client.GetUserID(ctx, channel)
	if err != nil {
		return nil, err
	}
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}
	pageSize := 100
	if maxCount > 0 && maxCount < pageSize {
		pageSize = maxCount
	}
	after := ""
	if maxAge == 0 {
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='catalog_after'`).Scan(&after)
	}
	collected := []VOD{}
	for maxCount == 0 || len(collected) < maxCount {
		videos, cursor, err := client.ListVideos(ctx, userID, after, pageSize)
		if err != nil {
			return nil, err
		}
		if len(videos) == 0 {
			break
		}
		for _, v := range videos {
			created, _ := time.Parse(time.RFC3339, v.CreatedAt)
			vodObj := VOD{ID: v.ID, Title: v.Title, Date: created, Duration: parseTwitchDuration(v.Duration)}
			if !cutoff.IsZero() && vodObj.Date.Before(cutoff) {
				return collected, nil
			}
			collected = append(collected, vodObj)
			if maxCount > 0 && len(collected) >= maxCount {
				break
			}
		}
		if cursor == "" || (maxCount > 0 && len(collected) >= maxCount) {
			break
		}
		after = cursor
		if maxAge == 0 {
			_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('catalog_after',$1,NOW())
				ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, after)
		}
		select {
		case <-ctx.Done():
			return collected, ctx.Err()
		case <-time.After(1200 * time.Millisecond):
		}
	}
	return collected, nil
}

// BackfillCatalog inserts historical VODs without marking processed.
func BackfillCatalog(ctx context.Context, db *sql.DB, maxCount int, maxAge time.Duration) error {
	vods, err := FetchAllChannelVODs(ctx, db, maxCount, maxAge)
	if err != nil {
		return err
	}
	for _, v := range vods {
		_, _ = db.ExecContext(ctx, `INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at) VALUES ($1,$2,$3,$4,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, v.ID, v.Title, v.Date, v.Duration)
	}
	slog.Info("catalog backfill inserted/ignored", slog.Int("count", len(vods)))
	return nil
}

// StartVODCatalogBackfillJob periodically backfills older VODs.
func StartVODCatalogBackfillJob(ctx context.Context, db *sql.DB) {
	interval := 6 * time.Hour
	if v := os.Getenv("VOD_CATALOG_BACKFILL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}
	maxCount := 0
	if s := os.Getenv("VOD_CATALOG_MAX"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			maxCount = n
		}
	}
	maxAge := time.Duration(0)
	if s := os.Getenv("VOD_CATALOG_MAX_AGE_DAYS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			maxAge = time.Duration(n) * 24 * time.Hour
		}
	}
	slog.Info("catalog backfill job starting", slog.Duration("interval", interval), slog.Int("max", maxCount), slog.Duration("max_age", maxAge))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	_ = BackfillCatalog(ctx, db, maxCount, maxAge)
	for {
		select {
		case <-ctx.Done():
			slog.Info("catalog backfill job stopped")
			return
		case <-ticker.C:
			if err := BackfillCatalog(ctx, db, maxCount, maxAge); err != nil {
				slog.Warn("catalog backfill", slog.Any("err", err))
			}
		}
	}
}

// parseTwitchDuration parses Twitch duration format like "3h15m42s".
func parseTwitchDuration(s string) int {
	var total int
	cur := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			cur += string(r)
			continue
		}
		if cur == "" {
			continue
		}
		n := 0
		for _, d := range cur {
			n = n*10 + int(d-'0')
		}
		switch r {
		case 'h':
			total += n * 3600
		case 'm':
			total += n * 60
		case 's':
			total += n
		}
		cur = ""
	}
	return total
}

// helixClient returns a shared HelixClient initialized from env.
func helixClient() *twitchapi.HelixClient {
	return &twitchapi.HelixClient{AppTokenSource: &twitchapi.TokenSource{ClientID: os.Getenv("TWITCH_CLIENT_ID"), ClientSecret: os.Getenv("TWITCH_CLIENT_SECRET")}, ClientID: os.Getenv("TWITCH_CLIENT_ID")}
}
