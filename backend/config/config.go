package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
    // Twitch
    TwitchChannel     string
    TwitchBotUsername string
    TwitchOAuthToken  string

    // VOD
    TwitchVODID    string
    TwitchVODStart time.Time

    // Database
    DBDsn string
}

// Load reads environment variables and applies defaults. It doesn't fail if Twitch creds are missing;
// use Validate() when you require chat recording.
func Load() (*Config, error) {
    cfg := &Config{}

    cfg.TwitchChannel = os.Getenv("TWITCH_CHANNEL")
    cfg.TwitchBotUsername = os.Getenv("TWITCH_BOT_USERNAME")
    cfg.TwitchOAuthToken = os.Getenv("TWITCH_OAUTH_TOKEN")

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
        cfg.DBDsn = "vodtender.db"
    }

    return cfg, nil
}

// Validate checks required fields when chat is enabled.
func (c *Config) ValidateChatReady() error {
    if c.TwitchChannel == "" || c.TwitchBotUsername == "" || c.TwitchOAuthToken == "" {
        return fmt.Errorf("missing twitch env: require TWITCH_CHANNEL, TWITCH_BOT_USERNAME, TWITCH_OAUTH_TOKEN")
    }
    return nil
}
