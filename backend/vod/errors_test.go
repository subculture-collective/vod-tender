package vod

import (
	"errors"
	"testing"
)

func TestErrorClassString(t *testing.T) {
	tests := []struct {
		class ErrorClass
		want  string
	}{
		{ErrorClassRetryable, "retryable"},
		{ErrorClassFatal, "fatal"},
		{ErrorClassUnknown, "unknown"},
		{ErrorClass(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.class.String()
			if got != tt.want {
				t.Errorf("ErrorClass.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyDownloadError_Fatal(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		// Authentication/Authorization errors
		{"subscriber-only", errors.New("This video is subscriber-only")},
		{"must be logged in", errors.New("ERROR: You must be logged into an account to download this video")},
		{"login required", errors.New("login required to access this content")},
		{"authentication required", errors.New("Authentication required for this resource")},
		{"401 unauthorized", errors.New("HTTP Error 401: Unauthorized")},
		{"403 forbidden", errors.New("HTTP Error 403: Forbidden")},
		{"access denied", errors.New("Access denied to video content")},
		{"unauthorized", errors.New("Unauthorized access")},

		// Content not found errors
		{"404 not found", errors.New("HTTP Error 404: Not Found")},
		{"video unavailable", errors.New("This video is unavailable")},
		{"video not available", errors.New("Video not available in your region")},
		{"video deleted", errors.New("Video has been deleted by the creator")},
		{"no longer available", errors.New("This video is no longer available")},
		{"video does not exist", errors.New("Video does not exist")},
		{"no formats", errors.New("ERROR: No video formats found; please report this issue")},
		{"unable to extract", errors.New("Unable to extract video information")},

		// Invalid input errors
		{"invalid url", errors.New("ERROR: Invalid URL provided")},
		{"malformed url", errors.New("Malformed URL structure")},
		{"invalid video id", errors.New("Invalid video ID format")},
		{"unsupported url", errors.New("Unsupported URL scheme")},

		// DRM/protection errors
		{"drm protected", errors.New("This content is DRM protected")},
		{"protected content", errors.New("Protected content cannot be downloaded")},
		{"encrypted content", errors.New("Encrypted content requires decryption keys")},

		// Case insensitive matching
		{"uppercase SUBSCRIBER", errors.New("THIS VIDEO IS SUBSCRIBER-ONLY")},
		{"mixed case Not Found", errors.New("Video Not Found")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyDownloadError(tt.err)
			if got != ErrorClassFatal {
				t.Errorf("ClassifyDownloadError(%q) = %v, want %v", tt.err, got, ErrorClassFatal)
			}
		})
	}
}

func TestClassifyDownloadError_Retryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		// Network errors
		{"connection reset", errors.New("connection reset by peer")},
		{"connection refused", errors.New("connection refused")},
		{"connection timeout", errors.New("connection timed out")},
		{"generic timeout", errors.New("operation timed out after 30s")},
		{"dns failure", errors.New("temporary failure in name resolution")},
		{"no route to host", errors.New("no route to host")},
		{"network unreachable", errors.New("network is unreachable")},
		{"dns error", errors.New("DNS lookup failed")},
		{"eof", errors.New("unexpected EOF while reading response")},
		{"broken pipe", errors.New("write: broken pipe")},

		// Server errors
		{"500 internal error", errors.New("HTTP Error 500: Internal Server Error")},
		{"502 bad gateway", errors.New("HTTP Error 502: Bad Gateway")},
		{"503 unavailable", errors.New("HTTP Error 503: Service Unavailable")},
		{"504 timeout", errors.New("HTTP Error 504: Gateway Timeout")},
		{"internal server error text", errors.New("Server returned: internal server error")},

		// Rate limiting
		{"429 rate limit", errors.New("HTTP Error 429: Too Many Requests")},
		{"too many requests", errors.New("Too many requests, please try again later")},
		{"rate limited", errors.New("Request rate limit exceeded")},
		{"throttled", errors.New("API throttled, retry after delay")},

		// Incomplete downloads
		{"partial content", errors.New("Partial content received, incomplete download")},
		{"fragment error", errors.New("Failed to download fragment 42")},
		{"incomplete download", errors.New("Download incomplete, missing data")},

		// Case insensitive matching
		{"uppercase TIMEOUT", errors.New("CONNECTION TIMED OUT")},
		{"mixed case Network", errors.New("Network Unreachable Error")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyDownloadError(tt.err)
			if got != ErrorClassRetryable {
				t.Errorf("ClassifyDownloadError(%q) = %v, want %v", tt.err, got, ErrorClassRetryable)
			}
		})
	}
}

func TestClassifyDownloadError_Unknown(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{"nil error", nil, ErrorClassUnknown},
		{"empty error", errors.New(""), ErrorClassRetryable}, // Empty defaults to retryable
		{"unknown error", errors.New("something completely unexpected happened"), ErrorClassRetryable},
		{"generic error", errors.New("an error occurred"), ErrorClassRetryable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyDownloadError(tt.err)
			if got != tt.want {
				t.Errorf("ClassifyDownloadError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"retryable network error", errors.New("connection timeout"), true},
		{"fatal auth error", errors.New("subscriber-only"), false},
		{"nil error", nil, false},
		{"unknown error defaults to retryable", errors.New("weird error"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryableError(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryableError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsFatalError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"fatal auth error", errors.New("subscriber-only"), true},
		{"fatal not found", errors.New("404 not found"), true},
		{"retryable network error", errors.New("connection timeout"), false},
		{"nil error", nil, false},
		{"unknown error", errors.New("weird error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFatalError(tt.err)
			if got != tt.want {
				t.Errorf("IsFatalError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestErrorClassificationTable provides a comprehensive overview of error patterns.
func TestErrorClassificationTable(t *testing.T) {
	// This test documents the complete classification table
	table := []struct {
		category string
		class    ErrorClass
		patterns []string
	}{
		{
			category: "Authentication/Authorization",
			class:    ErrorClassFatal,
			patterns: []string{
				"subscriber-only",
				"login required",
				"401",
				"403",
			},
		},
		{
			category: "Content Not Found",
			class:    ErrorClassFatal,
			patterns: []string{
				"404",
				"video unavailable",
				"deleted",
			},
		},
		{
			category: "Invalid Input",
			class:    ErrorClassFatal,
			patterns: []string{
				"invalid url",
				"invalid video id",
			},
		},
		{
			category: "DRM Protected",
			class:    ErrorClassFatal,
			patterns: []string{
				"drm protected",
				"encrypted content",
			},
		},
		{
			category: "Network Errors",
			class:    ErrorClassRetryable,
			patterns: []string{
				"timeout",
				"connection reset",
				"dns",
			},
		},
		{
			category: "Server Errors",
			class:    ErrorClassRetryable,
			patterns: []string{
				"500",
				"502",
				"503",
			},
		},
		{
			category: "Rate Limiting",
			class:    ErrorClassRetryable,
			patterns: []string{
				"429",
				"rate limit",
			},
		},
	}

	for _, tc := range table {
		t.Run(tc.category, func(t *testing.T) {
			for _, pattern := range tc.patterns {
				err := errors.New(pattern)
				got := ClassifyDownloadError(err)
				if got != tc.class {
					t.Errorf("%s: pattern %q classified as %v, want %v",
						tc.category, pattern, got, tc.class)
				}
			}
		})
	}
}
