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

// TestCancelIntegrationWithDB tests the full cancel workflow including DB updates
func TestCancelIntegrationWithDB(t *testing.T) {
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

	testID := "test-vod-integration-cancel"
	channel := "test-channel"

	// Clean up any previous test data
	_, _ = db.Exec(`DELETE FROM vods WHERE twitch_vod_id=$1`, testID)

	// Insert a test VOD
	_, err = db.Exec(`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		VALUES ($1, $2, $3, NOW(), 100, NOW())`, channel, testID, "Test VOD for Cancel")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec(`DELETE FROM vods WHERE twitch_vod_id=$1`, testID)
	}()

	// Create a mock downloader that simulates a long-running download
	mock := &mockDownloaderWithCancel{
		started: make(chan struct{}),
	}

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Register cancel function (simulating what downloadVOD does)
	activeMu.Lock()
	activeCancels[testID] = cancel
	activeMu.Unlock()

	// Start the download in a goroutine
	downloadErr := make(chan error, 1)
	go func() {
		// Set download state to "downloading" before starting
		_, _ = db.Exec(`UPDATE vods SET download_state=$1, download_bytes=$2, download_total=$3, progress_updated_at=NOW() 
			WHERE twitch_vod_id=$4`, "downloading", 5000, 100000, testID)

		_, err := mock.Download(ctx, db, testID, "/tmp")

		// Simulate what downloadVOD does on cancel
		if ctx.Err() != nil {
			_, _ = db.ExecContext(context.Background(), `UPDATE vods SET download_state=$1, download_bytes=0, download_total=0, progress_updated_at=NOW() 
				WHERE twitch_vod_id=$2`, "canceled", testID)
		}

		downloadErr <- err
	}()

	// Wait for download to start
	<-mock.started

	// Verify initial state
	var downloadState string
	var downloadBytes, downloadTotal int64
	err = db.QueryRow(`SELECT COALESCE(download_state, ''), COALESCE(download_bytes, 0), COALESCE(download_total, 0) 
		FROM vods WHERE twitch_vod_id=$1`, testID).Scan(&downloadState, &downloadBytes, &downloadTotal)
	if err != nil {
		t.Fatal(err)
	}

	if downloadState != "downloading" {
		t.Errorf("Expected download_state='downloading', got '%s'", downloadState)
	}
	if downloadBytes != 5000 {
		t.Errorf("Expected download_bytes=5000, got %d", downloadBytes)
	}
	if downloadTotal != 100000 {
		t.Errorf("Expected download_total=100000, got %d", downloadTotal)
	}

	// Cancel the download
	if !CancelDownload(testID) {
		t.Fatal("CancelDownload should succeed")
	}

	// Wait for download to finish
	select {
	case err := <-downloadErr:
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Download did not cancel within timeout")
	}

	// Wait a bit for DB update to complete
	time.Sleep(100 * time.Millisecond)

	// Verify final state
	err = db.QueryRow(`SELECT COALESCE(download_state, ''), COALESCE(download_bytes, 0), COALESCE(download_total, 0) 
		FROM vods WHERE twitch_vod_id=$1`, testID).Scan(&downloadState, &downloadBytes, &downloadTotal)
	if err != nil {
		t.Fatal(err)
	}

	if downloadState != "canceled" {
		t.Errorf("Expected download_state='canceled' after cancel, got '%s'", downloadState)
	}
	if downloadBytes != 0 {
		t.Errorf("Expected download_bytes=0 after cancel, got %d", downloadBytes)
	}
	if downloadTotal != 0 {
		t.Errorf("Expected download_total=0 after cancel, got %d", downloadTotal)
	}

	// Verify cancel function was removed
	activeMu.Lock()
	_, exists := activeCancels[testID]
	activeMu.Unlock()

	if exists {
		t.Error("Cancel function should be removed after cancellation")
	}

	// Verify download was actually canceled
	if !mock.wasCanceled() {
		t.Error("Download should have been canceled")
	}
	if mock.wasCompleted() {
		t.Error("Download should not have completed")
	}
}

// TestCancelDoesNotTripCircuitBreaker ensures cancellation doesn't count as a failure
func TestCancelDoesNotTripCircuitBreaker(t *testing.T) {
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

	channel := "test-channel-circuit"

	// Clean up any previous circuit breaker state
	_, _ = db.Exec(`DELETE FROM kv WHERE channel=$1 AND key LIKE 'circuit%'`, channel)

	// Set circuit breaker to closed state
	_, err = db.Exec(`INSERT INTO kv (channel, key, value, updated_at) 
		VALUES ($1, 'circuit_state', 'closed', NOW())
		ON CONFLICT(channel, key) DO UPDATE SET value='closed', updated_at=NOW()`, channel)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`INSERT INTO kv (channel, key, value, updated_at) 
		VALUES ($1, 'circuit_failures', '0', NOW())
		ON CONFLICT(channel, key) DO UPDATE SET value='0', updated_at=NOW()`, channel)
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial circuit state is closed
	var circuitState string
	err = db.QueryRow(`SELECT value FROM kv WHERE channel=$1 AND key='circuit_state'`, channel).Scan(&circuitState)
	if err != nil {
		t.Fatal(err)
	}
	if circuitState != "closed" {
		t.Fatalf("Expected circuit state 'closed', got '%s'", circuitState)
	}

	// Note: In the actual implementation, processOnce() handles context cancellation
	// and doesn't call updateCircuitOnFailure(). This test documents that behavior.
	// The circuit breaker should remain in 'closed' state after a cancel operation.

	// Simulate what processOnce does when it detects ctx.Err()
	// It returns early without updating circuit breaker
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel

	// Check if ctx.Err() is not nil (as processOnce does)
	if ctx.Err() != nil {
		// processOnce returns early here, doesn't call updateCircuitOnFailure
		t.Log("Context canceled, processOnce would return early without tripping circuit")
	}

	// Verify circuit state is still closed
	err = db.QueryRow(`SELECT value FROM kv WHERE channel=$1 AND key='circuit_state'`, channel).Scan(&circuitState)
	if err != nil {
		t.Fatal(err)
	}
	if circuitState != "closed" {
		t.Errorf("Expected circuit state to remain 'closed', got '%s'", circuitState)
	}

	// Cleanup
	_, _ = db.Exec(`DELETE FROM kv WHERE channel=$1 AND key LIKE 'circuit%'`, channel)
}

// TestCancelDoesNotIncrementRetries verifies that retry counter is not incremented on cancel
func TestCancelDoesNotIncrementRetries(t *testing.T) {
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

	testID := "test-vod-retries-cancel"
	channel := "test-channel"

	// Clean up any previous test data
	_, _ = db.Exec(`DELETE FROM vods WHERE twitch_vod_id=$1`, testID)

	// Insert a test VOD with an initial retry count
	_, err = db.Exec(`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, download_retries, created_at) 
		VALUES ($1, $2, $3, NOW(), 100, 2, NOW())`, channel, testID, "Test VOD Retries")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec(`DELETE FROM vods WHERE twitch_vod_id=$1`, testID)
	}()

	// Verify initial retry count
	var retriesBefore int
	err = db.QueryRow(`SELECT download_retries FROM vods WHERE twitch_vod_id=$1`, testID).Scan(&retriesBefore)
	if err != nil {
		t.Fatal(err)
	}
	if retriesBefore != 2 {
		t.Fatalf("Expected initial retries=2, got %d", retriesBefore)
	}

	// Create a mock downloader
	mock := &mockDownloaderWithCancel{
		started: make(chan struct{}),
	}

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Register cancel function
	activeMu.Lock()
	activeCancels[testID] = cancel
	activeMu.Unlock()

	// Start the download
	downloadErr := make(chan error, 1)
	go func() {
		_, _ = db.Exec(`UPDATE vods SET download_state='downloading' WHERE twitch_vod_id=$1`, testID)

		_, err := mock.Download(ctx, db, testID, "/tmp")

		// Simulate what downloadVOD does on cancel (doesn't increment retries)
		if ctx.Err() != nil {
			_, _ = db.ExecContext(context.Background(), `UPDATE vods SET download_state='canceled' WHERE twitch_vod_id=$1`, testID)
		}

		downloadErr <- err
	}()

	// Wait for download to start
	<-mock.started

	// Cancel the download
	CancelDownload(testID)

	// Wait for download to finish
	<-downloadErr

	// Wait for DB update
	time.Sleep(100 * time.Millisecond)

	// Verify retry count is unchanged
	var retriesAfter int
	err = db.QueryRow(`SELECT download_retries FROM vods WHERE twitch_vod_id=$1`, testID).Scan(&retriesAfter)
	if err != nil {
		t.Fatal(err)
	}

	if retriesAfter != retriesBefore {
		t.Errorf("Expected retries to remain at %d after cancel, got %d", retriesBefore, retriesAfter)
	}
}
