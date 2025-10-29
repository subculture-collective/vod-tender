package vod

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

func TestDownloadConcurrency(t *testing.T) {
	// Reset semaphore for test isolation
	downloadSemaphoreOnce = sync.Once{}
	downloadSemaphore = nil
	
	// Set max concurrent to 2 for testing
	os.Setenv("MAX_CONCURRENT_DOWNLOADS", "2")
	defer os.Unsetenv("MAX_CONCURRENT_DOWNLOADS")
	
	// Initialize
	initDownloadSemaphore()
	
	if cap := GetMaxConcurrentDownloads(); cap != 2 {
		t.Fatalf("expected max concurrent downloads 2, got %d", cap)
	}
	
	ctx := context.Background()
	
	// Acquire first slot
	if !acquireDownloadSlot(ctx) {
		t.Fatal("failed to acquire first slot")
	}
	if active := GetActiveDownloads(); active != 1 {
		t.Fatalf("expected 1 active download, got %d", active)
	}
	
	// Acquire second slot
	if !acquireDownloadSlot(ctx) {
		t.Fatal("failed to acquire second slot")
	}
	if active := GetActiveDownloads(); active != 2 {
		t.Fatalf("expected 2 active downloads, got %d", active)
	}
	
	// Third should block - test with timeout
	ctx2, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if acquireDownloadSlot(ctx2) {
		t.Fatal("should not have acquired third slot")
	}
	
	// Release one slot
	releaseDownloadSlot()
	if active := GetActiveDownloads(); active != 1 {
		t.Fatalf("expected 1 active download after release, got %d", active)
	}
	
	// Now third should succeed
	if !acquireDownloadSlot(ctx) {
		t.Fatal("failed to acquire slot after release")
	}
	
	// Clean up
	releaseDownloadSlot()
	releaseDownloadSlot()
}

func TestDownloadConcurrencyDefault(t *testing.T) {
	// Reset semaphore for test isolation
	downloadSemaphoreOnce = sync.Once{}
	downloadSemaphore = nil
	
	// Don't set env var - should default to 1
	os.Unsetenv("MAX_CONCURRENT_DOWNLOADS")
	
	initDownloadSemaphore()
	
	if cap := GetMaxConcurrentDownloads(); cap != 1 {
		t.Fatalf("expected default max concurrent downloads 1, got %d", cap)
	}
}

func TestDownloadConcurrencyContextCancel(t *testing.T) {
	// Reset semaphore for test isolation
	downloadSemaphoreOnce = sync.Once{}
	downloadSemaphore = nil
	
	os.Setenv("MAX_CONCURRENT_DOWNLOADS", "1")
	defer os.Unsetenv("MAX_CONCURRENT_DOWNLOADS")
	
	initDownloadSemaphore()
	
	// Acquire the only slot
	ctx := context.Background()
	if !acquireDownloadSlot(ctx) {
		t.Fatal("failed to acquire slot")
	}
	
	// Cancel context before trying to acquire
	ctx2, cancel := context.WithCancel(ctx)
	cancel()
	
	if acquireDownloadSlot(ctx2) {
		t.Fatal("should not have acquired slot with canceled context")
	}
	
	// Clean up
	releaseDownloadSlot()
}
