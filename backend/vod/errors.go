package vod

import (
	"strings"
)

// ErrorClass represents whether an error should be retried or not.
type ErrorClass int

const (
	// ErrorClassRetryable indicates the operation should be retried (transient errors).
	ErrorClassRetryable ErrorClass = iota
	// ErrorClassFatal indicates the operation should not be retried (permanent errors).
	ErrorClassFatal
	// ErrorClassUnknown indicates the error type cannot be determined.
	ErrorClassUnknown
)

// String returns a human-readable name for the error class.
func (ec ErrorClass) String() string {
	switch ec {
	case ErrorClassRetryable:
		return "retryable"
	case ErrorClassFatal:
		return "fatal"
	case ErrorClassUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// ClassifyDownloadError classifies download errors into retryable vs fatal categories.
//
// Fatal errors (non-retryable):
// - Authentication/authorization errors (subscriber-only, login required, 403/401)
// - Content not found errors (404, video unavailable, deleted)
// - Invalid input errors (malformed URL, invalid video ID)
// - DRM/protection errors (protected content)
//
// Retryable errors (transient):
// - Network errors (connection reset, timeout, DNS failures)
// - Server errors (500, 502, 503, 504)
// - Rate limiting (429, too many requests)
// - Incomplete downloads (partial file, fragment errors)
// - Temporary Twitch API errors
//
// Unknown errors:
// - Errors that don't match known patterns (treated as retryable for safety)
func ClassifyDownloadError(err error) ErrorClass {
	if err == nil {
		return ErrorClassUnknown
	}

	errMsg := err.Error()
	lower := strings.ToLower(errMsg)

	// Check retryable server errors first (before more generic patterns)
	// Server errors: 500, 502, 503, 504
	if strings.Contains(lower, "500") ||
		strings.Contains(lower, "502") ||
		strings.Contains(lower, "503") ||
		strings.Contains(lower, "504") ||
		strings.Contains(lower, "internal server error") ||
		strings.Contains(lower, "bad gateway") ||
		strings.Contains(lower, "service unavailable") ||
		strings.Contains(lower, "gateway timeout") {
		return ErrorClassRetryable
	}

	// Fatal errors: Authentication and authorization
	// Check these before other patterns to prioritize auth errors
	if strings.Contains(lower, "subscriber-only") ||
		strings.Contains(lower, "only available to subscribers") ||
		strings.Contains(lower, "must be logged into") ||
		strings.Contains(lower, "login required") ||
		strings.Contains(lower, "authentication required") ||
		strings.Contains(lower, "401") ||
		strings.Contains(lower, "403") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "unauthorized") {
		return ErrorClassFatal
	}

	// Fatal errors: Content not found or unavailable
	// Note: "service unavailable" (503) was already handled above as retryable
	// Check for "video" + "unavailable" or "video" + "not available"
	if (strings.Contains(lower, "video") && strings.Contains(lower, "unavailable")) ||
		(strings.Contains(lower, "video") && strings.Contains(lower, "not available")) ||
		strings.Contains(lower, "404") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "deleted") ||
		strings.Contains(lower, "no longer available") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(lower, "no video formats found") ||
		strings.Contains(lower, "unable to extract") {
		return ErrorClassFatal
	}

	// Fatal errors: Invalid input
	invalidInputPatterns := []string{
		"invalid url",
		"malformed url",
		"invalid video id",
		"unsupported url",
	}
	for _, pattern := range invalidInputPatterns {
		if strings.Contains(lower, pattern) {
			return ErrorClassFatal
		}
	}

	// Fatal errors: DRM and content protection
	drmPatterns := []string{
		"drm protected",
		"protected content",
		"encrypted content",
	}
	for _, pattern := range drmPatterns {
		if strings.Contains(lower, pattern) {
			return ErrorClassFatal
		}
	}

	// Retryable errors: Network issues
	networkPatterns := []string{
		"connection reset",
		"connection refused",
		"connection timed out",
		"timeout",
		"temporary failure in name resolution",
		"no route to host",
		"network unreachable",
		"dns",
		"eof",
		"broken pipe",
	}
	for _, pattern := range networkPatterns {
		if strings.Contains(lower, pattern) {
			return ErrorClassRetryable
		}
	}

	// Server errors already handled above

	// Retryable errors: Rate limiting
	rateLimitPatterns := []string{
		"429",
		"too many requests",
		"rate limit",
		"throttled",
	}
	for _, pattern := range rateLimitPatterns {
		if strings.Contains(lower, pattern) {
			return ErrorClassRetryable
		}
	}

	// Retryable errors: Incomplete downloads
	incompletePatterns := []string{
		"partial content",
		"fragment",
		"incomplete download",
	}
	for _, pattern := range incompletePatterns {
		if strings.Contains(lower, pattern) {
			return ErrorClassRetryable
		}
	}

	// Default: unknown errors are treated as retryable to avoid giving up too early
	return ErrorClassRetryable
}

// IsRetryableError checks if an error should trigger retry logic.
func IsRetryableError(err error) bool {
	return ClassifyDownloadError(err) == ErrorClassRetryable
}

// IsFatalError checks if an error should not be retried.
func IsFatalError(err error) bool {
	return ClassifyDownloadError(err) == ErrorClassFatal
}
