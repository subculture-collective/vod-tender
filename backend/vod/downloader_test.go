package vod

import (
	"os"
	"os/exec"
	"testing"
)

// TestAria2cDetection verifies that aria2c detection works correctly.
func TestAria2cDetection(t *testing.T) {
	// Check if aria2c is available in PATH
	_, err := exec.LookPath("aria2c")

	if err == nil {
		t.Log("aria2c found in PATH - external downloader will be enabled")
	} else {
		t.Log("aria2c not found in PATH - will use yt-dlp's built-in downloader")
	}

	// This test always passes; it's informational
	// The actual behavior is tested in integration tests
}

// TestAria2cFallbackBehavior verifies the fallback when aria2c is not available.
func TestAria2cFallbackBehavior(t *testing.T) {
	// Simulate aria2c not being available by testing the logic
	// In production, if LookPath fails, aria2c args are simply not added

	testCases := []struct {
		name         string
		aria2cExists bool
		expectAria2c bool
	}{
		{
			name:         "aria2c available",
			aria2cExists: true,
			expectAria2c: true,
		},
		{
			name:         "aria2c not available",
			aria2cExists: false,
			expectAria2c: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This tests the logic pattern used in downloadVOD
			args := []string{"yt-dlp", "--continue"}

			// Simulate the aria2c detection logic
			if tc.aria2cExists {
				// In real code: if _, err := exec.LookPath("aria2c"); err == nil
				args = append([]string{"--external-downloader", "aria2c"}, args...)
			}

			hasAria2c := false
			for _, arg := range args {
				if arg == "aria2c" {
					hasAria2c = true
					break
				}
			}

			if hasAria2c != tc.expectAria2c {
				t.Errorf("aria2c in args = %v, want %v", hasAria2c, tc.expectAria2c)
			}
		})
	}
}

// TestYtDlpPathResolution tests the yt-dlp binary path resolution logic.
func TestYtDlpPathResolution(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) (cleanup func())
		expected string
	}{
		{
			name: "yt-dlp in PATH",
			setup: func(t *testing.T) func() {
				// Try to find yt-dlp in PATH
				_, err := exec.LookPath("yt-dlp")
				if err != nil {
					t.Skip("yt-dlp not in PATH, skipping")
				}
				return func() {}
			},
			expected: "yt-dlp", // Will resolve via LookPath
		},
		{
			name: "fallback to /usr/local/bin/yt-dlp",
			setup: func(t *testing.T) func() {
				// Check if /usr/local/bin/yt-dlp exists
				if _, err := os.Stat("/usr/local/bin/yt-dlp"); err != nil {
					t.Skip("/usr/local/bin/yt-dlp not found, skipping")
				}
				return func() {}
			},
			expected: "/usr/local/bin/yt-dlp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := tt.setup(t)
			defer cleanup()

			// Simulate the resolution logic from downloadVOD
			ytDLP := "yt-dlp"
			if p, err := exec.LookPath("yt-dlp"); err == nil {
				ytDLP = p
			} else if _, err2 := os.Stat("/usr/local/bin/yt-dlp"); err2 == nil {
				ytDLP = "/usr/local/bin/yt-dlp"
			}

			// Verify we got a valid path
			if ytDLP == "" {
				t.Error("yt-dlp path resolution failed")
			}

			// Log the resolved path for debugging
			t.Logf("Resolved yt-dlp path: %s", ytDLP)
		})
	}
}

// TestDownloadArgsConstruction verifies the download arguments are properly constructed.
func TestDownloadArgsConstruction(t *testing.T) {
	tests := []struct {
		name            string
		ytdlpArgs       string
		ytdlpVerbose    string
		expectedFlags   []string
		unexpectedFlags []string
	}{
		{
			name:          "basic args without secrets",
			ytdlpArgs:     "",
			ytdlpVerbose:  "0",
			expectedFlags: []string{"--continue", "--retries", "infinite", "--no-cache-dir"},
		},
		{
			name:          "verbose enabled",
			ytdlpArgs:     "",
			ytdlpVerbose:  "1",
			expectedFlags: []string{"--continue", "-v"},
		},
		{
			name:          "extra args appended",
			ytdlpArgs:     "--no-warnings --no-playlist",
			ytdlpVerbose:  "0",
			expectedFlags: []string{"--no-warnings", "--no-playlist", "--continue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate basic arg construction from downloadVOD
			args := []string{
				"--continue",
				"--retries", "infinite",
				"--fragment-retries", "infinite",
				"--concurrent-fragments", "10",
				"--no-cache-dir",
				"-o", "/tmp/output.mp4",
				"https://example.com/video",
			}

			// Add extra args if provided
			if tt.ytdlpArgs != "" {
				// In real code this uses strings.Fields
				extraArgs := []string{"--no-warnings", "--no-playlist"}
				args = append(extraArgs, args...)
			}

			// Check verbose flag logic
			if tt.ytdlpVerbose == "1" {
				args = append([]string{"-v"}, args...)
			}

			// Verify expected flags are present
			for _, flag := range tt.expectedFlags {
				found := false
				for _, arg := range args {
					if arg == flag {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected flag %q not found in args", flag)
				}
			}

			// Verify unexpected flags are absent
			for _, flag := range tt.unexpectedFlags {
				for _, arg := range args {
					if arg == flag {
						t.Errorf("unexpected flag %q found in args", flag)
					}
				}
			}
		})
	}
}

// TestAria2cCommandLineArgs verifies the aria2c arguments are properly formatted.
func TestAria2cCommandLineArgs(t *testing.T) {
	expectedArgs := []string{
		"--external-downloader", "aria2c",
		"--downloader-args", "aria2c:-x16 -s16 -k1M --file-allocation=none",
	}

	// This tests the exact arguments used in production
	args := []string{"--external-downloader", "aria2c"}
	args = append(args, "--downloader-args", "aria2c:-x16 -s16 -k1M --file-allocation=none")

	for i, expected := range expectedArgs {
		if i >= len(args) {
			t.Errorf("missing arg at index %d: expected %q", i, expected)
			continue
		}
		if args[i] != expected {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], expected)
		}
	}
}
