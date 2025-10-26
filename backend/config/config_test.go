package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultChannelConstant(t *testing.T) {
	// Verify DefaultChannel is an empty string as expected for legacy single-channel mode
	if DefaultChannel != "" {
		t.Errorf("DefaultChannel = %q, want empty string for legacy compatibility", DefaultChannel)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TWITCH_VOD_ID", "")
	t.Setenv("TWITCH_VOD_START", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.TwitchVODID == "" {
		t.Errorf("expected default vod id, got empty")
	}
	if time.Since(cfg.TwitchVODStart) > time.Minute {
		t.Errorf("unexpected default vod start too old: %v", cfg.TwitchVODStart)
	}
}

func TestValidateChatReady(t *testing.T) {
	t.Setenv("TWITCH_CHANNEL", "chan")
	t.Setenv("TWITCH_BOT_USERNAME", "bot")
	t.Setenv("TWITCH_OAUTH_TOKEN", "oauth:token")
	cfg, _ := Load()
	if err := cfg.ValidateChatReady(); err != nil {
		t.Errorf("expected valid chat config, got %v", err)
	}
	if err := os.Unsetenv("TWITCH_CHANNEL"); err != nil {
		t.Fatalf("failed to unset TWITCH_CHANNEL: %v", err)
	}
	cfg, _ = Load()
	if err := cfg.ValidateChatReady(); err == nil {
		t.Errorf("expected error when missing twitch envs")
	}
}

func TestValidateChatReadyWithDefaultChannel(t *testing.T) {
	// Test that DefaultChannel (empty string) fails validation
	t.Setenv("TWITCH_CHANNEL", DefaultChannel)
	t.Setenv("TWITCH_BOT_USERNAME", "bot")
	t.Setenv("TWITCH_OAUTH_TOKEN", "oauth:token")
	cfg, _ := Load()
	if err := cfg.ValidateChatReady(); err == nil {
		t.Errorf("expected error when TWITCH_CHANNEL is DefaultChannel (empty), but validation passed")
	}
}
