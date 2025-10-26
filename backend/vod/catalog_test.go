package vod

import (
	"context"
	"testing"
	"time"

	"github.com/onnwee/vod-tender/backend/config"
)

func TestParseTwitchDurationEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty string", "", 0},
		{"hours only", "2h", 7200},
		{"minutes only", "45m", 2700},
		{"seconds only", "30s", 30},
		{"all components", "1h2m3s", 3723},
		{"large hours", "10h", 36000},
		{"zero values", "0h0m0s", 0},
		{"no units", "123", 0},
		{"mixed with no numbers", "h1m2s3", 62},
		{"duplicate hours", "1h2h", 10800}, // 1*3600 + 2*3600
		{"reversed order", "3s2m1h", 3723},
		{"with spaces (should ignore)", "1h 2m 3s", 3723},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTwitchDuration(tt.input)
			if got != tt.want {
				t.Errorf("parseTwitchDuration(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFetchChannelVODsEmpty(t *testing.T) {
	// Test when TWITCH_CHANNEL is not set (using DefaultChannel sentinel)
	t.Setenv("TWITCH_CHANNEL", config.DefaultChannel)
	ctx := context.Background()
	vods, err := FetchChannelVODs(ctx, "")
	if err != nil {
		t.Errorf("FetchChannelVODs() error = %v, want nil", err)
	}
	if vods != nil {
		t.Errorf("FetchChannelVODs() = %v, want nil", vods)
	}
}

func TestVODStruct(t *testing.T) {
	// Test VOD struct initialization
	now := time.Now()
	vod := VOD{
		ID:       "123456",
		Title:    "Test Stream",
		Date:     now,
		Duration: 3600,
	}

	if vod.ID != "123456" {
		t.Errorf("VOD.ID = %s, want %s", vod.ID, "123456")
	}
	if vod.Title != "Test Stream" {
		t.Errorf("VOD.Title = %s, want %s", vod.Title, "Test Stream")
	}
	if !vod.Date.Equal(now) {
		t.Errorf("VOD.Date = %v, want %v", vod.Date, now)
	}
	if vod.Duration != 3600 {
		t.Errorf("VOD.Duration = %d, want %d", vod.Duration, 3600)
	}
}
