// Package config loads environment variables and provides a typed Config used across the service.
// It applies sensible defaults so the binary can run locally with minimal setup.
// For required credentials (e.g., Twitch chat), use ValidateChatReady.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	// Twitch
	TwitchChannel      string
	TwitchBotUsername  string
	TwitchOAuthToken   string
	TwitchClientID     string
	TwitchClientSecret string
	TwitchRedirectURI  string
	TwitchScopes       string

	// VOD
	TwitchVODID    string
	TwitchVODStart time.Time

	// Database
	DBDsn string

	// Storage
	DataDir string

	// YouTube OAuth
	YTClientID     string
	YTClientSecret string
	YTRedirectURI  string
	YTScopes       string
}

// Load reads environment variables and applies defaults. It doesn't fail if Twitch creds are missing;
// use ValidateChatReady() when you require chat recording. Missing optional variables disable features (e.g., YouTube).
func Load() (*Config, error) {
	cfg := &Config{}

	cfg.TwitchChannel = os.Getenv("TWITCH_CHANNEL")
	cfg.TwitchBotUsername = os.Getenv("TWITCH_BOT_USERNAME")
	cfg.TwitchOAuthToken = os.Getenv("TWITCH_OAUTH_TOKEN")
	cfg.TwitchClientID = os.Getenv("TWITCH_CLIENT_ID")
	cfg.TwitchClientSecret = os.Getenv("TWITCH_CLIENT_SECRET")
	cfg.TwitchRedirectURI = os.Getenv("TWITCH_REDIRECT_URI")
	cfg.TwitchScopes = os.Getenv("TWITCH_SCOPES")
	if cfg.TwitchScopes == "" {
		// default scopes for chat bot
		cfg.TwitchScopes = "chat:read chat:edit"
	}

	// VOD
	cfg.TwitchVODID = os.Getenv("TWITCH_VOD_ID")
	if cfg.TwitchVODID == "" {
		cfg.TwitchVODID = "demo-vod-id"
	}

	if v := os.Getenv("TWITCH_VOD_START"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("invalid TWITCH_VOD_START (RFC3339): %w", err)
		}
		cfg.TwitchVODStart = t.UTC()
	} else {
		cfg.TwitchVODStart = time.Now().UTC()
	}

	// DB
	cfg.DBDsn = os.Getenv("DB_DSN")
	if cfg.DBDsn == "" {
		// Default to local Postgres (matches docker-compose). Legacy sqlite filename removed.
		cfg.DBDsn = "postgres://vod:vod@localhost:5432/vod?sslmode=disable"
	}

	// Storage
	cfg.DataDir = os.Getenv("DATA_DIR")
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}

	// YouTube
	cfg.YTClientID = os.Getenv("YT_CLIENT_ID")
	cfg.YTClientSecret = os.Getenv("YT_CLIENT_SECRET")
	cfg.YTRedirectURI = os.Getenv("YT_REDIRECT_URI")
	cfg.YTScopes = os.Getenv("YT_SCOPES")
	if cfg.YTScopes == "" {
		cfg.YTScopes = "https://www.googleapis.com/auth/youtube.upload"
	}

	return cfg, nil
}

// ValidateChatReady checks required fields when chat is enabled (manual recorder path).
func (c *Config) ValidateChatReady() error {
	if c.TwitchChannel == "" || c.TwitchBotUsername == "" || c.TwitchOAuthToken == "" {
		return fmt.Errorf("missing twitch env: require TWITCH_CHANNEL, TWITCH_BOT_USERNAME, TWITCH_OAUTH_TOKEN")
	}
	return nil
}
