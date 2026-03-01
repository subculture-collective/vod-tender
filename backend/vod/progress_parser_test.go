package vod

import (
	"math"
	"testing"
)

func TestParseByteQuantity(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
		ok    bool
	}{
		{name: "bytes", input: "1024B", want: 1024, ok: true},
		{name: "kib", input: "1KiB", want: 1024, ok: true},
		{name: "mib", input: "1.5MiB", want: 1572864, ok: true},
		{name: "gib", input: "2GiB", want: 2147483648, ok: true},
		{name: "tib", input: "1TiB", want: 1099511627776, ok: true},
		{name: "decimal mb", input: "1MB", want: 1000000, ok: true},
		{name: "invalid", input: "abc", want: 0, ok: false},
		{name: "unknown unit", input: "1PiB", want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseByteQuantity(tt.input)
			if ok != tt.ok {
				t.Fatalf("parseByteQuantity(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("parseByteQuantity(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDownloadProgressLine_Structured(t *testing.T) {
	line := "VT_PROGRESS| 50.0%|1048576|0|524288|12345|12"

	parsed, ok := parseDownloadProgressLine(line)
	if !ok {
		t.Fatalf("parseDownloadProgressLine() ok = false, want true")
	}
	if parsed.Percent == nil || math.Abs(*parsed.Percent-50.0) > 0.0001 {
		t.Fatalf("percent = %v, want 50.0", parsed.Percent)
	}
	if parsed.TotalBytes != 1048576 {
		t.Fatalf("totalBytes = %d, want 1048576", parsed.TotalBytes)
	}
	if parsed.DownloadedBytes != 524288 {
		t.Fatalf("downloadedBytes = %d, want 524288", parsed.DownloadedBytes)
	}
}

func TestParseDownloadProgressLine_StructuredEstimatedTotalFallback(t *testing.T) {
	line := "VT_PROGRESS| 25%|NA|2097152|NA|NA|NA"

	parsed, ok := parseDownloadProgressLine(line)
	if !ok {
		t.Fatalf("parseDownloadProgressLine() ok = false, want true")
	}
	if parsed.TotalBytes != 2097152 {
		t.Fatalf("totalBytes = %d, want 2097152", parsed.TotalBytes)
	}
	if parsed.DownloadedBytes != 524288 {
		t.Fatalf("downloadedBytes = %d, want 524288", parsed.DownloadedBytes)
	}
}

func TestParseDownloadProgressLine_LegacyFormat(t *testing.T) {
	line := "[download]   4.3% of ~2.19GiB at  3.05MiB/s ETA 11:22"

	parsed, ok := parseDownloadProgressLine(line)
	if !ok {
		t.Fatalf("parseDownloadProgressLine() ok = false, want true")
	}
	if parsed.Percent == nil || math.Abs(*parsed.Percent-4.3) > 0.0001 {
		t.Fatalf("percent = %v, want 4.3", parsed.Percent)
	}
	if parsed.TotalBytes <= 0 {
		t.Fatalf("totalBytes = %d, want > 0", parsed.TotalBytes)
	}
	if parsed.DownloadedBytes <= 0 {
		t.Fatalf("downloadedBytes = %d, want > 0", parsed.DownloadedBytes)
	}
}

func TestParseDownloadProgressLine_Malformed(t *testing.T) {
	tests := []string{
		"",
		"random log line",
		"VT_PROGRESS|N/A|NA|NA|NA|NA|NA",
	}

	for _, line := range tests {
		t.Run(line, func(t *testing.T) {
			_, ok := parseDownloadProgressLine(line)
			if ok {
				t.Fatalf("parseDownloadProgressLine(%q) ok = true, want false", line)
			}
		})
	}
}
