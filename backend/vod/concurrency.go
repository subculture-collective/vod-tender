package vod

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
)

// downloadSemaphore limits concurrent downloads globally across all processing jobs.
// It is initialized once based on MAX_CONCURRENT_DOWNLOADS env var (default: 1 for serial processing).
var (
	downloadSemaphore     chan struct{}
	downloadSemaphoreOnce sync.Once
)

// initDownloadSemaphore initializes the global download semaphore based on MAX_CONCURRENT_DOWNLOADS.
func initDownloadSemaphore() {
	downloadSemaphoreOnce.Do(func() {
		maxConcurrent := 1 // default: serial processing
		if s := os.Getenv("MAX_CONCURRENT_DOWNLOADS"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				maxConcurrent = n
			}
		}
		downloadSemaphore = make(chan struct{}, maxConcurrent)
		slog.Info("download concurrency limit initialized", slog.Int("max_concurrent", maxConcurrent))
	})
}

// acquireDownloadSlot blocks until a download slot is available or context is canceled.
// Returns true if slot acquired, false if context canceled.
func acquireDownloadSlot(ctx context.Context) bool {
	initDownloadSemaphore()
	select {
	case downloadSemaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// releaseDownloadSlot releases a download slot, allowing another download to proceed.
func releaseDownloadSlot() {
	initDownloadSemaphore()
	select {
	case <-downloadSemaphore:
	default:
		// Should not happen unless mismatched acquire/release
		slog.Warn("download slot release called without corresponding acquire")
	}
}

// GetActiveDownloads returns the current number of active downloads.
func GetActiveDownloads() int {
	initDownloadSemaphore()
	return len(downloadSemaphore)
}

// GetMaxConcurrentDownloads returns the configured maximum concurrent downloads.
func GetMaxConcurrentDownloads() int {
	initDownloadSemaphore()
	return cap(downloadSemaphore)
}
