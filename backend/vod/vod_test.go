package vod

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestCancelDownload(t *testing.T) {
	// Test canceling non-existent download
	if CancelDownload("nonexistent") {
		t.Error("CancelDownload should return false for non-existent ID")
	}

	// Test that we can register and cancel a download
	testID := "test-vod-123"
	called := false
	cancelFunc := func() {
		called = true
	}

	activeMu.Lock()
	activeCancels[testID] = cancelFunc
	activeMu.Unlock()

	if !CancelDownload(testID) {
		t.Error("CancelDownload should return true for existing ID")
	}

	if !called {
		t.Error("Cancel function was not called")
	}

	// Verify it's been removed
	activeMu.Lock()
	_, exists := activeCancels[testID]
	activeMu.Unlock()

	if exists {
		t.Error("Canceled download should be removed from active map")
	}

	// Test canceling again should return false
	if CancelDownload(testID) {
		t.Error("CancelDownload should return false after already canceled")
	}
}

func TestVODStructFields(t *testing.T) {
	// Verify VOD struct has expected fields and can be created
	vod := VOD{
		ID:       "123",
		Title:    "Test",
		Duration: 100,
	}

	if vod.ID != "123" {
		t.Errorf("VOD.ID = %s, want 123", vod.ID)
	}
	if vod.Title != "Test" {
		t.Errorf("VOD.Title = %s, want Test", vod.Title)
	}
	if vod.Duration != 100 {
		t.Errorf("VOD.Duration = %d, want 100", vod.Duration)
	}
}

func TestDownloaderInterface(t *testing.T) {
	// Verify we can create a mock downloader that implements the interface
	type mockDownloaderTest struct{}
	func (m *mockDownloaderTest) Download(ctx context.Context, dbc *sql.DB, id, dataDir string) (string, error) {
		return "/tmp/test.mp4", nil
	}
	
	var _ Downloader = (*mockDownloaderTest)(nil) // Compile-time interface check
}

func TestUploaderInterface(t *testing.T) {
	// Verify we can create a mock uploader that implements the interface
	type mockUploaderTest struct{}
	func (m *mockUploaderTest) Upload(ctx context.Context, path, title string, date time.Time) (string, error) {
		return "https://youtube.com/watch?v=123", nil
	}
	
	var _ Uploader = (*mockUploaderTest)(nil) // Compile-time interface check
}
