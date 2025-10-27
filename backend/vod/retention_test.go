package vod

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

func TestLoadRetentionPolicy(t *testing.T) {
	// Save and restore env vars
	oldKeepDays := os.Getenv("RETENTION_KEEP_DAYS")
	oldKeepCount := os.Getenv("RETENTION_KEEP_COUNT")
	oldDryRun := os.Getenv("RETENTION_DRY_RUN")
	oldInterval := os.Getenv("RETENTION_INTERVAL")
	defer func() {
		os.Setenv("RETENTION_KEEP_DAYS", oldKeepDays)
		os.Setenv("RETENTION_KEEP_COUNT", oldKeepCount)
		os.Setenv("RETENTION_DRY_RUN", oldDryRun)
		os.Setenv("RETENTION_INTERVAL", oldInterval)
	}()

	tests := []struct {
		name          string
		keepDays      string
		keepCount     string
		dryRun        string
		interval      string
		wantDays      int
		wantCount     int
		wantDryRun    bool
		wantInterval  time.Duration
	}{
		{
			name:         "defaults",
			wantInterval: 6 * time.Hour,
		},
		{
			name:         "keep_days_only",
			keepDays:     "30",
			wantDays:     30,
			wantInterval: 6 * time.Hour,
		},
		{
			name:         "keep_count_only",
			keepCount:    "100",
			wantCount:    100,
			wantInterval: 6 * time.Hour,
		},
		{
			name:         "both_policies",
			keepDays:     "7",
			keepCount:    "50",
			wantDays:     7,
			wantCount:    50,
			wantInterval: 6 * time.Hour,
		},
		{
			name:         "dry_run_enabled",
			keepDays:     "14",
			dryRun:       "1",
			wantDays:     14,
			wantDryRun:   true,
			wantInterval: 6 * time.Hour,
		},
		{
			name:         "custom_interval",
			keepDays:     "7",
			interval:     "12h",
			wantDays:     7,
			wantInterval: 12 * time.Hour,
		},
		{
			name:         "invalid_values_ignored",
			keepDays:     "invalid",
			keepCount:    "-5",
			interval:     "not-a-duration",
			wantInterval: 6 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("RETENTION_KEEP_DAYS", tt.keepDays)
			os.Setenv("RETENTION_KEEP_COUNT", tt.keepCount)
			os.Setenv("RETENTION_DRY_RUN", tt.dryRun)
			os.Setenv("RETENTION_INTERVAL", tt.interval)

			policy := LoadRetentionPolicy()

			if policy.KeepLastNDays != tt.wantDays {
				t.Errorf("KeepLastNDays = %d, want %d", policy.KeepLastNDays, tt.wantDays)
			}
			if policy.KeepLastNVODs != tt.wantCount {
				t.Errorf("KeepLastNVODs = %d, want %d", policy.KeepLastNVODs, tt.wantCount)
			}
			if policy.DryRun != tt.wantDryRun {
				t.Errorf("DryRun = %v, want %v", policy.DryRun, tt.wantDryRun)
			}
			if policy.Interval != tt.wantInterval {
				t.Errorf("Interval = %v, want %v", policy.Interval, tt.wantInterval)
			}
		})
	}
}

func TestRunRetentionCleanupByDays(t *testing.T) {
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

	ctx := context.Background()
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Create temp data dir
	tmpDir := t.TempDir()
	oldDataDir := os.Getenv("DATA_DIR")
	os.Setenv("DATA_DIR", tmpDir)
	defer os.Setenv("DATA_DIR", oldDataDir)

	channel := "test_retention"

	// Clean up test data
	_, _ = db.ExecContext(ctx, `DELETE FROM vods WHERE channel=$1`, channel)

	// Insert test VODs spanning 15 days
	now := time.Now()
	vodDates := []struct {
		id   string
		date time.Time
		path string
	}{
		{"old1", now.Add(-14 * 24 * time.Hour), filepath.Join(tmpDir, "old1.mp4")},
		{"old2", now.Add(-10 * 24 * time.Hour), filepath.Join(tmpDir, "old2.mp4")},
		{"recent1", now.Add(-5 * 24 * time.Hour), filepath.Join(tmpDir, "recent1.mp4")},
		{"recent2", now.Add(-2 * 24 * time.Hour), filepath.Join(tmpDir, "recent2.mp4")},
		{"today", now, filepath.Join(tmpDir, "today.mp4")},
	}

	for _, vod := range vodDates {
		// Create dummy file
		if err := os.WriteFile(vod.path, []byte("test data"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := db.ExecContext(ctx, `
			INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, downloaded_path, processed, created_at)
			VALUES ($1, $2, $3, $4, 60, $5, true, NOW())
			ON CONFLICT (twitch_vod_id) DO UPDATE SET 
				date=EXCLUDED.date, 
				downloaded_path=EXCLUDED.downloaded_path,
				channel=EXCLUDED.channel
		`, channel, vod.id, "Test "+vod.id, vod.date, vod.path)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Run retention with keep_days=7 (should remove old1 and old2)
	policy := RetentionPolicy{
		KeepLastNDays: 7,
		DryRun:        false,
	}

	if err := runRetentionCleanup(ctx, db, channel, policy); err != nil {
		t.Fatal(err)
	}

	// Verify old files are deleted
	for _, vod := range vodDates {
		exists := true
		if _, err := os.Stat(vod.path); os.IsNotExist(err) {
			exists = false
		}

		daysOld := int(now.Sub(vod.date).Hours() / 24)
		shouldExist := daysOld <= 7

		if exists != shouldExist {
			t.Errorf("File %s (age=%dd): exists=%v, want=%v", vod.id, daysOld, exists, shouldExist)
		}

		// Check DB is updated
		var path sql.NullString
		err := db.QueryRowContext(ctx, `SELECT downloaded_path FROM vods WHERE twitch_vod_id=$1`, vod.id).Scan(&path)
		if err != nil {
			t.Fatal(err)
		}

		if shouldExist {
			if !path.Valid {
				t.Errorf("VOD %s should have path in DB", vod.id)
			}
		} else {
			if path.Valid {
				t.Errorf("VOD %s should not have path in DB", vod.id)
			}
		}
	}
}

func TestRunRetentionCleanupByCount(t *testing.T) {
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

	ctx := context.Background()
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	oldDataDir := os.Getenv("DATA_DIR")
	os.Setenv("DATA_DIR", tmpDir)
	defer os.Setenv("DATA_DIR", oldDataDir)

	channel := "test_retention_count"
	_, _ = db.ExecContext(ctx, `DELETE FROM vods WHERE channel=$1`, channel)

	// Insert 5 VODs, keep only last 3
	now := time.Now()
	for i := 0; i < 5; i++ {
		id := "vod" + string(rune('0'+i))
		path := filepath.Join(tmpDir, id+".mp4")
		date := now.Add(-time.Duration(4-i) * 24 * time.Hour)

		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := db.ExecContext(ctx, `
			INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, downloaded_path, processed, created_at)
			VALUES ($1, $2, $3, $4, 60, $5, true, NOW())
			ON CONFLICT (twitch_vod_id) DO UPDATE SET 
				date=EXCLUDED.date, 
				downloaded_path=EXCLUDED.downloaded_path,
				channel=EXCLUDED.channel
		`, channel, id, "Test "+id, date, path)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Keep last 3 VODs
	policy := RetentionPolicy{
		KeepLastNVODs: 3,
		DryRun:        false,
	}

	if err := runRetentionCleanup(ctx, db, channel, policy); err != nil {
		t.Fatal(err)
	}

	// Count remaining files
	var remaining int
	rows, err := db.QueryContext(ctx, `SELECT COUNT(*) FROM vods WHERE channel=$1 AND downloaded_path IS NOT NULL`, channel)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("failed to close rows: %v", err)
		}
	}()
	if rows.Next() {
		if err := rows.Scan(&remaining); err != nil {
			t.Fatal(err)
		}
	}

	if remaining != 3 {
		t.Errorf("Expected 3 VODs with files remaining, got %d", remaining)
	}
}

func TestRunRetentionCleanupDryRun(t *testing.T) {
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

	ctx := context.Background()
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	oldDataDir := os.Getenv("DATA_DIR")
	os.Setenv("DATA_DIR", tmpDir)
	defer os.Setenv("DATA_DIR", oldDataDir)

	channel := "test_retention_dryrun"
	_, _ = db.ExecContext(ctx, `DELETE FROM vods WHERE channel=$1`, channel)

	// Create old VOD
	id := "old_vod"
	path := filepath.Join(tmpDir, "old_vod.mp4")
	date := time.Now().Add(-30 * 24 * time.Hour)

	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, downloaded_path, processed, created_at)
		VALUES ($1, $2, $3, $4, 60, $5, true, NOW())
		ON CONFLICT (twitch_vod_id) DO UPDATE SET 
			date=EXCLUDED.date, 
			downloaded_path=EXCLUDED.downloaded_path,
			channel=EXCLUDED.channel
	`, channel, id, "Test old", date, path)
	if err != nil {
		t.Fatal(err)
	}

	// Run in dry-run mode
	policy := RetentionPolicy{
		KeepLastNDays: 7,
		DryRun:        true,
	}

	if err := runRetentionCleanup(ctx, db, channel, policy); err != nil {
		t.Fatal(err)
	}

	// File should still exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Dry-run mode should not delete files")
	}

	// DB should still have path
	var dbPath sql.NullString
	err = db.QueryRowContext(ctx, `SELECT downloaded_path FROM vods WHERE twitch_vod_id=$1`, id).Scan(&dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !dbPath.Valid {
		t.Error("Dry-run mode should not update DB")
	}
}

func TestRunRetentionCleanupSkipsActiveVODs(t *testing.T) {
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

	ctx := context.Background()
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	oldDataDir := os.Getenv("DATA_DIR")
	os.Setenv("DATA_DIR", tmpDir)
	defer os.Setenv("DATA_DIR", oldDataDir)

	channel := "test_retention_active"
	_, _ = db.ExecContext(ctx, `DELETE FROM vods WHERE channel=$1`, channel)

	// Create old VOD that is currently being processed
	id := "active_vod"
	path := filepath.Join(tmpDir, "active_vod.mp4")
	date := time.Now().Add(-30 * 24 * time.Hour)

	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, downloaded_path, processed, download_state, created_at)
		VALUES ($1, $2, $3, $4, 60, $5, false, 'downloading', NOW())
		ON CONFLICT (twitch_vod_id) DO UPDATE SET 
			date=EXCLUDED.date, 
			downloaded_path=EXCLUDED.downloaded_path,
			processed=EXCLUDED.processed,
			download_state=EXCLUDED.download_state,
			channel=EXCLUDED.channel
	`, channel, id, "Test active", date, path)
	if err != nil {
		t.Fatal(err)
	}

	// Run retention - should skip active VOD even though it's old
	policy := RetentionPolicy{
		KeepLastNDays: 7,
		DryRun:        false,
	}

	if err := runRetentionCleanup(ctx, db, channel, policy); err != nil {
		t.Fatal(err)
	}

	// File should still exist (protected)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Active VOD should be protected from cleanup")
	}

	// DB should still have path
	var dbPath sql.NullString
	err = db.QueryRowContext(ctx, `SELECT downloaded_path FROM vods WHERE twitch_vod_id=$1`, id).Scan(&dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !dbPath.Valid {
		t.Error("Active VOD path should not be cleared")
	}
}

func TestCleanupTempFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create various temp files with different ages
	now := time.Now()
	files := []struct {
		name    string
		age     time.Duration
		isTemp  bool
		wantDel bool
	}{
		{"video.mp4", 0, false, false},              // Regular file, shouldn't touch
		{"old.part", 25 * time.Hour, true, true},    // Old partial, should delete
		{"new.part", 1 * time.Hour, true, false},    // New partial, should keep
		{"old.tmp", 30 * time.Hour, true, true},     // Old temp, should delete
		{"new.tmp", 12 * time.Hour, true, false},    // New temp, should keep
		{"video.transcode.tmp.mp4", 26 * time.Hour, true, true}, // Old transcode temp, should delete
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f.name)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		// Set modification time
		modTime := now.Add(-f.age)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}

	// Run cleanup with 24h threshold
	CleanupTempFiles(tmpDir, 24*time.Hour)

	// Check results
	for _, f := range files {
		path := filepath.Join(tmpDir, f.name)
		_, err := os.Stat(path)
		exists := !os.IsNotExist(err)

		if f.wantDel && exists {
			t.Errorf("File %s should have been deleted", f.name)
		}
		if !f.wantDel && !exists {
			t.Errorf("File %s should not have been deleted", f.name)
		}
	}
}
