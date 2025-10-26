package vod

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

func TestMultiChannelIsolation(t *testing.T) {
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
	
	// Insert VODs for two different channels
	channel1 := "channel1"
	channel2 := "channel2"
	
	_, err = db.ExecContext(ctx, `INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		VALUES ($1, 'vod1', 'Channel 1 VOD', NOW(), 60, NOW()) 
		ON CONFLICT (twitch_vod_id) DO NOTHING`, channel1)
	if err != nil {
		t.Fatalf("failed to insert vod for channel1: %v", err)
	}
	
	_, err = db.ExecContext(ctx, `INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		VALUES ($1, 'vod2', 'Channel 2 VOD', NOW(), 60, NOW()) 
		ON CONFLICT (twitch_vod_id) DO NOTHING`, channel2)
	if err != nil {
		t.Fatalf("failed to insert vod for channel2: %v", err)
	}
	
	// Verify isolation: channel1 query should only return channel1 VODs
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE channel = $1`, channel1).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query vods for channel1: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least 1 VOD for channel1, got %d", count)
	}
	
	// Verify channel2 has its own VODs
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE channel = $1`, channel2).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query vods for channel2: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least 1 VOD for channel2, got %d", count)
	}
	
	// Verify kv isolation
	_, err = db.ExecContext(ctx, `INSERT INTO kv (channel, key, value, updated_at) 
		VALUES ($1, 'test_key', 'value1', NOW()) 
		ON CONFLICT (channel, key) DO UPDATE SET value = EXCLUDED.value`, channel1)
	if err != nil {
		t.Fatalf("failed to insert kv for channel1: %v", err)
	}
	
	_, err = db.ExecContext(ctx, `INSERT INTO kv (channel, key, value, updated_at) 
		VALUES ($1, 'test_key', 'value2', NOW()) 
		ON CONFLICT (channel, key) DO UPDATE SET value = EXCLUDED.value`, channel2)
	if err != nil {
		t.Fatalf("failed to insert kv for channel2: %v", err)
	}
	
	// Verify each channel has its own kv value
	var value1, value2 string
	err = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel = $1 AND key = 'test_key'`, channel1).Scan(&value1)
	if err != nil {
		t.Fatalf("failed to query kv for channel1: %v", err)
	}
	
	err = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE channel = $1 AND key = 'test_key'`, channel2).Scan(&value2)
	if err != nil {
		t.Fatalf("failed to query kv for channel2: %v", err)
	}
	
	if value1 != "value1" {
		t.Errorf("expected value1='value1', got %s", value1)
	}
	if value2 != "value2" {
		t.Errorf("expected value2='value2', got %s", value2)
	}
}

func TestBackwardCompatibilityEmptyChannel(t *testing.T) {
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
	
	// Insert VOD with empty channel (backward compatibility)
	channel := ""
	_, err = db.ExecContext(ctx, `INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		VALUES ($1, 'vod_compat', 'Legacy VOD', NOW(), 60, NOW()) 
		ON CONFLICT (twitch_vod_id) DO NOTHING`, channel)
	if err != nil {
		t.Fatalf("failed to insert vod with empty channel: %v", err)
	}
	
	// Verify we can query it
	var title string
	err = db.QueryRowContext(ctx, `SELECT title FROM vods WHERE channel = $1 AND twitch_vod_id = 'vod_compat'`, channel).Scan(&title)
	if err != nil {
		t.Fatalf("failed to query vod with empty channel: %v", err)
	}
	
	if title != "Legacy VOD" {
		t.Errorf("expected title='Legacy VOD', got %s", title)
	}
}

func TestOAuthTokenChannelIsolation(t *testing.T) {
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
	
	// Insert OAuth tokens for different channels
	channel1 := "channel1"
	channel2 := "channel2"
	
	// Use a simple time for testing
	expiry1 := time.Now().Add(1 * time.Hour)
	err = dbpkg.UpsertOAuthTokenForChannel(ctx, db, "twitch", channel1, "access1", "refresh1", expiry1, "", "")
	if err != nil {
		t.Fatalf("failed to upsert oauth token for channel1: %v", err)
	}
	
	expiry2 := time.Now().Add(2 * time.Hour)
	err = dbpkg.UpsertOAuthTokenForChannel(ctx, db, "twitch", channel2, "access2", "refresh2", expiry2, "", "")
	if err != nil {
		t.Fatalf("failed to upsert oauth token for channel2: %v", err)
	}
	
	// Verify isolation
	access1, refresh1, _, _, err := dbpkg.GetOAuthTokenForChannel(ctx, db, "twitch", channel1)
	if err != nil {
		t.Fatalf("failed to get oauth token for channel1: %v", err)
	}
	
	access2, refresh2, _, _, err := dbpkg.GetOAuthTokenForChannel(ctx, db, "twitch", channel2)
	if err != nil {
		t.Fatalf("failed to get oauth token for channel2: %v", err)
	}
	
	if access1 != "access1" {
		t.Errorf("expected access1='access1', got %s", access1)
	}
	if refresh1 != "refresh1" {
		t.Errorf("expected refresh1='refresh1', got %s", refresh1)
	}
	if access2 != "access2" {
		t.Errorf("expected access2='access2', got %s", access2)
	}
	if refresh2 != "refresh2" {
		t.Errorf("expected refresh2='refresh2', got %s", refresh2)
	}
}
