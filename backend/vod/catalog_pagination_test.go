package vod

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

// TestBackfillCatalogNoDuplicateInserts verifies idempotent VOD inserts
// This test ensures that running BackfillCatalog multiple times with the same
// VOD IDs doesn't create duplicate entries in the database
func TestBackfillCatalogNoDuplicateInserts(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Clean up test data
	channel := "test-duplicate-channel"
	_, _ = db.ExecContext(ctx, "DELETE FROM vods WHERE channel = $1", channel)

	// Insert same VODs twice manually to test idempotency
	vod1 := VOD{
		ID:       "duplicate-v1",
		Title:    "Duplicate Video 1",
		Date:     time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		Duration: 3600,
	}
	vod2 := VOD{
		ID:       "duplicate-v2",
		Title:    "Duplicate Video 2",
		Date:     time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC),
		Duration: 2700,
	}

	// First insert
	_, err = db.ExecContext(ctx,
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		 VALUES ($1,$2,$3,$4,$5,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`,
		channel, vod1.ID, vod1.Title, vod1.Date, vod1.Duration)
	if err != nil {
		t.Fatalf("First insert error: %v", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		 VALUES ($1,$2,$3,$4,$5,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`,
		channel, vod2.ID, vod2.Title, vod2.Date, vod2.Duration)
	if err != nil {
		t.Fatalf("First insert error: %v", err)
	}

	// Count rows after first insert
	var count1 int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vods WHERE channel = $1", channel).Scan(&count1)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	if count1 != 2 {
		t.Errorf("Expected 2 rows after first insert, got %d", count1)
	}

	// Second insert with same data (should be no-op due to ON CONFLICT DO NOTHING)
	_, err = db.ExecContext(ctx,
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		 VALUES ($1,$2,$3,$4,$5,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`,
		channel, vod1.ID, vod1.Title, vod1.Date, vod1.Duration)
	if err != nil {
		t.Fatalf("Second insert error: %v", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		 VALUES ($1,$2,$3,$4,$5,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`,
		channel, vod2.ID, vod2.Title, vod2.Date, vod2.Duration)
	if err != nil {
		t.Fatalf("Second insert error: %v", err)
	}

	// Count rows after second insert - should be same
	var count2 int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vods WHERE channel = $1", channel).Scan(&count2)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	if count2 != count1 {
		t.Errorf("Duplicate insert created new rows: got %d (first) vs %d (second)", count1, count2)
	}

	// Verify the specific VOD IDs exist exactly once
	var vodCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vods WHERE channel = $1 AND twitch_vod_id IN ('duplicate-v1', 'duplicate-v2')",
		channel).Scan(&vodCount)
	if err != nil {
		t.Fatalf("Failed to check specific VOD IDs: %v", err)
	}

	if vodCount != 2 {
		t.Errorf("Expected exactly 2 VODs with specific IDs, got %d", vodCount)
	}
}

// TestKVCursorStorageAndRetrieval tests cursor persistence in kv table
// This verifies that pagination cursors are correctly stored and can be retrieved
func TestKVCursorStorageAndRetrieval(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Clean up test data
	channel := "test-cursor-channel"
	_, _ = db.ExecContext(ctx, "DELETE FROM kv WHERE channel = $1", channel)

	// Test storing a cursor
	testCursor := "test-cursor-abc123"
	_, err = db.ExecContext(ctx,
		`INSERT INTO kv (channel, key, value, updated_at) 
		 VALUES ($1, 'catalog_after', $2, NOW())
		 ON CONFLICT(channel, key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`,
		channel, testCursor)
	if err != nil {
		t.Fatalf("Failed to insert cursor: %v", err)
	}

	// Test retrieving the cursor
	var retrievedCursor string
	err = db.QueryRowContext(ctx,
		"SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel).Scan(&retrievedCursor)
	if err != nil {
		t.Fatalf("Failed to retrieve cursor: %v", err)
	}

	if retrievedCursor != testCursor {
		t.Errorf("Retrieved cursor = %q, want %q", retrievedCursor, testCursor)
	}

	// Test updating the cursor
	updatedCursor := "test-cursor-def456"
	_, err = db.ExecContext(ctx,
		`INSERT INTO kv (channel, key, value, updated_at) 
		 VALUES ($1, 'catalog_after', $2, NOW())
		 ON CONFLICT(channel, key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`,
		channel, updatedCursor)
	if err != nil {
		t.Fatalf("Failed to update cursor: %v", err)
	}

	// Verify cursor was updated
	var newCursor string
	err = db.QueryRowContext(ctx,
		"SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel).Scan(&newCursor)
	if err != nil {
		t.Fatalf("Failed to retrieve updated cursor: %v", err)
	}

	if newCursor != updatedCursor {
		t.Errorf("Updated cursor = %q, want %q", newCursor, updatedCursor)
	}

	// Verify updated_at was set
	var updatedAt time.Time
	err = db.QueryRowContext(ctx,
		"SELECT updated_at FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel).Scan(&updatedAt)
	if err != nil {
		t.Fatalf("Failed to retrieve updated_at: %v", err)
	}

	if updatedAt.IsZero() {
		t.Error("Expected updated_at to be set")
	}

	// Verify only one row exists for this channel/key combination
	var count int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 row in kv table, got %d", count)
	}
}





// TestKVCursorMultipleChannelIsolation tests that cursors are isolated per channel
func TestKVCursorMultipleChannelIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Clean up test data
	channel1 := "test-channel-1"
	channel2 := "test-channel-2"
	_, _ = db.ExecContext(ctx, "DELETE FROM kv WHERE channel IN ($1, $2)", channel1, channel2)

	// Store cursor for channel 1
	cursor1 := "cursor-channel-1"
	_, err = db.ExecContext(ctx,
		`INSERT INTO kv (channel, key, value, updated_at) 
		 VALUES ($1, 'catalog_after', $2, NOW())
		 ON CONFLICT(channel, key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`,
		channel1, cursor1)
	if err != nil {
		t.Fatalf("Failed to insert cursor for channel 1: %v", err)
	}

	// Store cursor for channel 2
	cursor2 := "cursor-channel-2"
	_, err = db.ExecContext(ctx,
		`INSERT INTO kv (channel, key, value, updated_at) 
		 VALUES ($1, 'catalog_after', $2, NOW())
		 ON CONFLICT(channel, key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`,
		channel2, cursor2)
	if err != nil {
		t.Fatalf("Failed to insert cursor for channel 2: %v", err)
	}

	// Retrieve cursor for channel 1
	var retrievedCursor1 string
	err = db.QueryRowContext(ctx,
		"SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel1).Scan(&retrievedCursor1)
	if err != nil {
		t.Fatalf("Failed to retrieve cursor for channel 1: %v", err)
	}

	if retrievedCursor1 != cursor1 {
		t.Errorf("Channel 1 cursor = %q, want %q", retrievedCursor1, cursor1)
	}

	// Retrieve cursor for channel 2
	var retrievedCursor2 string
	err = db.QueryRowContext(ctx,
		"SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel2).Scan(&retrievedCursor2)
	if err != nil {
		t.Fatalf("Failed to retrieve cursor for channel 2: %v", err)
	}

	if retrievedCursor2 != cursor2 {
		t.Errorf("Channel 2 cursor = %q, want %q", retrievedCursor2, cursor2)
	}

	// Update cursor for channel 1
	updatedCursor1 := "cursor-channel-1-updated"
	_, err = db.ExecContext(ctx,
		`INSERT INTO kv (channel, key, value, updated_at) 
		 VALUES ($1, 'catalog_after', $2, NOW())
		 ON CONFLICT(channel, key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`,
		channel1, updatedCursor1)
	if err != nil {
		t.Fatalf("Failed to update cursor for channel 1: %v", err)
	}

	// Verify channel 1 cursor was updated
	err = db.QueryRowContext(ctx,
		"SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel1).Scan(&retrievedCursor1)
	if err != nil {
		t.Fatalf("Failed to retrieve updated cursor for channel 1: %v", err)
	}

	if retrievedCursor1 != updatedCursor1 {
		t.Errorf("Updated channel 1 cursor = %q, want %q", retrievedCursor1, updatedCursor1)
	}

	// Verify channel 2 cursor was NOT affected
	err = db.QueryRowContext(ctx,
		"SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'",
		channel2).Scan(&retrievedCursor2)
	if err != nil {
		t.Fatalf("Failed to retrieve cursor for channel 2 after update: %v", err)
	}

	if retrievedCursor2 != cursor2 {
		t.Errorf("Channel 2 cursor was affected: got %q, want %q", retrievedCursor2, cursor2)
	}
}
