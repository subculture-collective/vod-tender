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

func TestValidateYouTubeUploadPolicyDisabled(t *testing.T) {
	t.Setenv("YOUTUBE_UPLOAD_ENABLED", "0")
	t.Setenv("YOUTUBE_UPLOAD_OWNERSHIP", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if err := cfg.ValidateYouTubeUploadPolicy(); err != nil {
		t.Fatalf("expected disabled upload policy to pass validation, got: %v", err)
	}
}

func TestValidateYouTubeUploadPolicyRequiresOwnership(t *testing.T) {
	t.Setenv("YOUTUBE_UPLOAD_ENABLED", "1")
	t.Setenv("YOUTUBE_UPLOAD_OWNERSHIP", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if err := cfg.ValidateYouTubeUploadPolicy(); err == nil {
		t.Fatal("expected validation error when upload is enabled without ownership declaration")
	}
}

func TestValidateYouTubeUploadPolicyAcceptedValues(t *testing.T) {
	tests := []string{"self", "authorized", "SELF", " Authorized "}

	for _, ownership := range tests {
		t.Run(ownership, func(t *testing.T) {
			t.Setenv("YOUTUBE_UPLOAD_ENABLED", "true")
			t.Setenv("YOUTUBE_UPLOAD_OWNERSHIP", ownership)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if err := cfg.ValidateYouTubeUploadPolicy(); err != nil {
				t.Fatalf("expected valid ownership %q, got error: %v", ownership, err)
			}
		})
	}
}
