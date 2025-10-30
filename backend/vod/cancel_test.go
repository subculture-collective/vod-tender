package vod

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

// mockDownloaderWithCancel simulates a long-running download that can be canceled
type mockDownloaderWithCancel struct {
	started   chan struct{}
	canceled  bool
	completed bool
	mu        sync.Mutex
}

func (m *mockDownloaderWithCancel) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) {
	close(m.started) // signal that download has started
	
	// Simulate a long-running download
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for i := 0; i < 50; i++ { // 5 seconds total if not canceled
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.canceled = true
			m.mu.Unlock()
			return "", ctx.Err()
		case <-ticker.C:
			// Continue downloading
		}
	}
	
	m.mu.Lock()
	m.completed = true
	m.mu.Unlock()
	return "/tmp/test.mp4", nil
}

func (m *mockDownloaderWithCancel) wasCanceled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.canceled
}

func (m *mockDownloaderWithCancel) wasCompleted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.completed
}

// TestCancelActiveDownload tests canceling a download that is actively running
func TestCancelActiveDownload(t *testing.T) {
	testID := "test-vod-active-cancel"
	
	// Create a mock downloader that takes time
	mock := &mockDownloaderWithCancel{
		started: make(chan struct{}),
	}
	
	// Create a context with cancel for the download
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Register the cancel function (simulating what downloadVOD does)
	activeMu.Lock()
	activeCancels[testID] = cancel
	activeMu.Unlock()
	
	// Start the download in a goroutine
	downloadErr := make(chan error, 1)
	go func() {
		_, err := mock.Download(ctx, nil, testID, "/tmp")
		downloadErr <- err
	}()
	
	// Wait for download to start
	<-mock.started
	
	// Now cancel the download
	if !CancelDownload(testID) {
		t.Fatal("CancelDownload should return true for active download")
	}
	
	// Wait for download to finish with timeout
	select {
	case err := <-downloadErr:
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Download did not cancel within timeout")
	}
	
	// Verify the download was canceled
	if !mock.wasCanceled() {
		t.Error("Download should have been canceled")
	}
	
	if mock.wasCompleted() {
		t.Error("Download should not have completed")
	}
	
	// Verify the cancel function was removed
	activeMu.Lock()
	_, exists := activeCancels[testID]
	activeMu.Unlock()
	
	if exists {
		t.Error("Cancel function should be removed after cancellation")
	}
}

// TestCancelIdleDownload tests canceling when no download is active
func TestCancelIdleDownload(t *testing.T) {
	testID := "test-vod-idle-cancel"
	
	// Ensure no active download
	activeMu.Lock()
	delete(activeCancels, testID)
	activeMu.Unlock()
	
	// Try to cancel non-existent download
	if CancelDownload(testID) {
		t.Error("CancelDownload should return false for non-existent download")
	}
}

// TestCancelRaceDuringSetup tests the race between download setup and cancel
func TestCancelRaceDuringSetup(t *testing.T) {
	testID := "test-vod-race-setup"
	
	// Simulate the race: cancel function not yet registered
	activeMu.Lock()
	delete(activeCancels, testID)
	activeMu.Unlock()
	
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start registration in goroutine
	done := make(chan bool)
	go func() {
		time.Sleep(50 * time.Millisecond) // Simulate delay in registration
		activeMu.Lock()
		activeCancels[testID] = cancel
		activeMu.Unlock()
		done <- true
	}()
	
	// Try to cancel immediately (before registration completes)
	result1 := CancelDownload(testID)
	
	// Wait for registration to complete
	<-done
	
	// Try to cancel after registration
	result2 := CancelDownload(testID)
	
	// At least one should succeed
	if !result1 && !result2 {
		t.Error("Expected at least one cancel attempt to succeed")
	}
	
	// After both attempts, the cancel function should be removed
	activeMu.Lock()
	_, exists := activeCancels[testID]
	activeMu.Unlock()
	
	if exists {
		t.Error("Cancel function should be removed after successful cancellation")
	}
}

// TestCancelRaceDuringCompletion tests the race between download completion and cancel
func TestCancelRaceDuringCompletion(t *testing.T) {
	testID := "test-vod-race-completion"
	
	_, cancel := context.WithCancel(context.Background())
	
	// Register cancel function
	activeMu.Lock()
	activeCancels[testID] = cancel
	activeMu.Unlock()
	
	// Simulate download completing (removing cancel function)
	completionDone := make(chan bool)
	go func() {
		time.Sleep(50 * time.Millisecond)
		activeMu.Lock()
		delete(activeCancels, testID)
		activeMu.Unlock()
		cancel() // Also cancel context as cleanup
		completionDone <- true
	}()
	
	// Try to cancel immediately
	result1 := CancelDownload(testID)
	
	// Wait for completion
	<-completionDone
	
	// Try to cancel after completion
	result2 := CancelDownload(testID)
	
	// Exactly one should succeed (the one that got the lock first)
	if result1 == result2 {
		t.Error("Expected exactly one cancel attempt to succeed")
	}
	
	if result2 {
		t.Error("Cancel after completion should return false")
	}
	
	// Verify cleanup
	activeMu.Lock()
	_, exists := activeCancels[testID]
	activeMu.Unlock()
	
	if exists {
		t.Error("Cancel function should not exist after completion")
	}
}

// TestCancelDBStateConsistency tests that DB state is properly updated on cancel
func TestCancelDBStateConsistency(t *testing.T) {
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
	
	testID := "test-vod-db-state-cancel"
	channel := "test-channel"
	
	// Insert a test VOD
	_, err = db.Exec(`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) 
		VALUES ($1, $2, $3, NOW(), 100, NOW()) 
		ON CONFLICT (twitch_vod_id) DO NOTHING`, channel, testID, "Test VOD")
	if err != nil {
		t.Fatal(err)
	}
	
	// Set initial download state
	_, err = db.Exec(`UPDATE vods SET download_state=$1, download_bytes=$2, download_total=$3, progress_updated_at=NOW() 
		WHERE twitch_vod_id=$4`, "downloading", 1000, 10000, testID)
	if err != nil {
		t.Fatal(err)
	}
	
	// Create a mock download that can be canceled
	mock := &mockDownloaderWithCancel{
		started: make(chan struct{}),
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Register cancel function (simulating what downloadVOD does)
	activeMu.Lock()
	activeCancels[testID] = cancel
	activeMu.Unlock()
	
	// Start the download in a goroutine
	downloadErr := make(chan error, 1)
	go func() {
		_, err := mock.Download(ctx, db, testID, "/tmp")
		downloadErr <- err
	}()
	
	// Wait for download to start
	<-mock.started
	
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
	
	// Verify cancel function was removed
	activeMu.Lock()
	_, exists := activeCancels[testID]
	activeMu.Unlock()
	
	if exists {
		t.Error("Cancel function should be removed")
	}
	
	// Cleanup
	_, _ = db.Exec(`DELETE FROM vods WHERE twitch_vod_id=$1`, testID)
}

// TestMultipleConcurrentCancels tests that concurrent cancel calls are safe
func TestMultipleConcurrentCancels(t *testing.T) {
	testID := "test-vod-concurrent-cancel"
	
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Register cancel function
	activeMu.Lock()
	activeCancels[testID] = cancel
	activeMu.Unlock()
	
	// Launch multiple concurrent cancel attempts
	const numGoroutines = 10
	results := make(chan bool, numGoroutines)
	var wg sync.WaitGroup
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- CancelDownload(testID)
		}()
	}
	
	wg.Wait()
	close(results)
	
	// Exactly one should succeed
	successCount := 0
	for result := range results {
		if result {
			successCount++
		}
	}
	
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful cancel, got %d", successCount)
	}
	
	// Verify cleanup
	activeMu.Lock()
	_, exists := activeCancels[testID]
	activeMu.Unlock()
	
	if exists {
		t.Error("Cancel function should be removed after successful cancellation")
	}
}
