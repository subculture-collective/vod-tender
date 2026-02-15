// Package db provides database connection helpers, schema migration, and small data access helpers.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx postgres driver registered as 'pgx'

	"github.com/onnwee/vod-tender/backend/crypto"
)

var (
	// encryptor is the global encryptor instance for OAuth token encryption
	encryptor     crypto.Encryptor
	encryptorOnce sync.Once
	errEncryptor  error
)

// initEncryptor initializes the global encryptor from ENCRYPTION_KEY environment variable.
// If ENCRYPTION_KEY is not set, encryption is disabled (encryption_version = 0).
// This is called lazily on first use.
func initEncryptor() {
	encryptorOnce.Do(func() {
		key := os.Getenv("ENCRYPTION_KEY")
		if key == "" {
			slog.Warn("ENCRYPTION_KEY not set, OAuth tokens will be stored in plaintext (not recommended for production)", slog.String("component", "db_encryption"))
			return
		}

		enc, err := crypto.NewAESEncryptor(key)
		if err != nil {
			errEncryptor = fmt.Errorf("failed to initialize encryption: %w", err)
			slog.Error("encryption initialization failed", slog.Any("error", errEncryptor), slog.String("component", "db_encryption"))
			return
		}

		encryptor = enc
		slog.Info("OAuth token encryption enabled (AES-256-GCM)", slog.String("component", "db_encryption"))
	})
}

// getEncryptor returns the global encryptor instance, initializing it if necessary.
// Returns nil if encryption is not configured (ENCRYPTION_KEY not set).
func getEncryptor() (crypto.Encryptor, error) {
	initEncryptor()
	if errEncryptor != nil {
		return nil, errEncryptor
	}
	return encryptor, nil
}

// Connect opens a Postgres connection using DB_DSN (or a sane default when running in Docker compose).
func Connect() (*sql.DB, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		//nolint:gosec // G101: Default DSN for local development in Docker Compose, not production credentials
		dsn = "postgres://vod:vod@postgres:5432/vod?sslmode=disable"
	}
	return sql.Open("pgx", dsn)
}

// Migrate applies idempotent schema changes for all required tables and indices.
//
// DEPRECATED: This function provides backward compatibility for deployments that predate
// the versioned migration system (golang-migrate). New schema changes should be created as
// versioned migrations in db/migrations/ directory.
//
// This function is used as a fallback when:
// 1. The schema_migrations table doesn't exist (old deployment)
// 2. Versioned migrations fail for any reason
//
// Execution order in main.go:
// 1. db.RunMigrations() - Try versioned migrations first (canonical)
// 2. db.Migrate() - Fallback if step 1 fails (backward compatibility)
//
// See docs/MIGRATIONS.md for full migration architecture and guidance.
func Migrate(ctx context.Context, db *sql.DB) error { return migratePostgres(ctx, db) }

func migratePostgres(ctx context.Context, db *sql.DB) error {
	// LEGACY MIGRATION SYSTEM - For backward compatibility only
	// New schema changes should be added as versioned migrations in db/migrations/
	// See docs/MIGRATIONS.md for guidance.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS vods (
			id SERIAL PRIMARY KEY,
			twitch_vod_id TEXT UNIQUE,
			title TEXT,
			date TIMESTAMPTZ,
			duration_seconds INTEGER,
			downloaded_path TEXT,
			download_state TEXT,
			download_retries INTEGER DEFAULT 0,
			download_bytes BIGINT DEFAULT 0,
			download_total BIGINT DEFAULT 0,
			progress_updated_at TIMESTAMPTZ,
			processed BOOLEAN DEFAULT FALSE,
			processing_error TEXT,
			youtube_url TEXT,
			description TEXT,
			priority INTEGER DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ
		)`,
		`ALTER TABLE vods ADD COLUMN IF NOT EXISTS description TEXT`,
		`ALTER TABLE vods ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id SERIAL PRIMARY KEY,
			vod_id TEXT NOT NULL REFERENCES vods(twitch_vod_id),
			username TEXT,
			message TEXT,
			abs_timestamp TIMESTAMPTZ,
			rel_timestamp DOUBLE PRECISION,
			badges TEXT,
			emotes TEXT,
			color TEXT,
			reply_to_id TEXT,
			reply_to_username TEXT,
			reply_to_message TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS oauth_tokens (
			provider TEXT PRIMARY KEY,
			access_token TEXT,
			refresh_token TEXT,
			expires_at TIMESTAMPTZ,
			scope TEXT,
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			encryption_version INTEGER DEFAULT 0,
			encryption_key_id TEXT
		)`,
		`ALTER TABLE oauth_tokens ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT ''`,
		// Drop old primary key and create new composite key for multi-channel support
		// Check if current primary key columns match desired (provider, channel); if not, recreate it
		`DO $$
		DECLARE
			current_cols TEXT;
		BEGIN
			-- Get current primary key column composition in key order (not attnum order)
			SELECT string_agg(a.attname, ',' ORDER BY COALESCE(array_position(i.indkey, a.attnum::smallint), 999)) INTO current_cols
			FROM   pg_index i
			JOIN   pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE  i.indrelid = 'oauth_tokens'::regclass
			AND    i.indisprimary;

			-- Only recreate if columns don't match desired composition
			IF current_cols IS DISTINCT FROM 'provider,channel' THEN
				-- Drop existing primary key if present
				IF EXISTS (
					SELECT 1 FROM pg_constraint 
					WHERE conname = 'oauth_tokens_pkey' 
					AND conrelid = 'oauth_tokens'::regclass
				) THEN
					ALTER TABLE oauth_tokens DROP CONSTRAINT oauth_tokens_pkey;
				END IF;
				-- Add new composite primary key
				ALTER TABLE oauth_tokens ADD PRIMARY KEY (provider, channel);
			END IF;
		END $$`,
		`CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`ALTER TABLE kv ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT ''`,
		// Drop old primary key and create new composite key for multi-channel support
		// Check if current primary key columns match desired (channel, key); if not, recreate it
		`DO $$
		DECLARE
			current_cols TEXT;
		BEGIN
			-- Get current primary key column composition in key order (not attnum order)
			SELECT string_agg(a.attname, ',' ORDER BY COALESCE(array_position(i.indkey, a.attnum::smallint), 999)) INTO current_cols
			FROM   pg_index i
			JOIN   pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE  i.indrelid = 'kv'::regclass
			AND    i.indisprimary;

			-- Only recreate if columns don't match desired composition
			IF current_cols IS DISTINCT FROM 'channel,key' THEN
				-- Drop existing primary key if present
				IF EXISTS (
					SELECT 1 FROM pg_constraint 
					WHERE conname = 'kv_pkey' 
					AND conrelid = 'kv'::regclass
				) THEN
					ALTER TABLE kv DROP CONSTRAINT kv_pkey;
				END IF;
				-- Add new composite primary key
				ALTER TABLE kv ADD PRIMARY KEY (channel, key);
			END IF;
		END $$`,
		// The following ALTER TABLE statements are for backward compatibility with pre-encryption schema installations.
		// They ensure that existing deployments have the new columns added if missing.
		`ALTER TABLE oauth_tokens ADD COLUMN IF NOT EXISTS encryption_version INTEGER DEFAULT 0`,
		`ALTER TABLE oauth_tokens ADD COLUMN IF NOT EXISTS encryption_key_id TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_vods_twitch_vod_id ON vods(twitch_vod_id)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_processed ON vods(processed)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_date ON vods(date)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_vod_rel ON chat_messages(vod_id, rel_timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_vod_abs ON chat_messages(vod_id, abs_timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_proc_pri_date ON vods(processed, priority, date)`,
		// Multi-channel support indices
		`CREATE INDEX IF NOT EXISTS idx_vods_channel_date ON vods(channel, date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_channel_processed ON vods(channel, processed, priority DESC, date ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_channel_vod ON chat_messages(channel, vod_id)`,
		// Rate limiter table for distributed rate limiting across multiple API replicas
		`CREATE TABLE IF NOT EXISTS rate_limit_requests (
			id BIGSERIAL PRIMARY KEY,
			ip TEXT NOT NULL,
			request_time TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rate_limit_ip_time ON rate_limit_requests(ip, request_time)`,
		`CREATE INDEX IF NOT EXISTS idx_rate_limit_time ON rate_limit_requests(request_time)`,
	}
	for i, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("postgres migrate step %d failed: %w", i, err)
		}
	}
	return nil
}

// UpsertOAuthToken stores or updates an OAuth token for a provider (e.g., twitch, youtube).
// Uses default channel (empty string) for backward compatibility.
// If encryption is enabled (ENCRYPTION_KEY set), tokens are encrypted before storage.
// encryption_version=1 indicates encrypted tokens, version=0 indicates plaintext.
func UpsertOAuthToken(ctx context.Context, dbx *sql.DB, provider, access, refresh string, expiry time.Time, raw string, scope string) error {
	return UpsertOAuthTokenForChannel(ctx, dbx, provider, "", access, refresh, expiry, raw, scope)
}

// UpsertOAuthTokenForChannel stores or updates an OAuth token for a provider and channel.
// If encryption is enabled (ENCRYPTION_KEY set), tokens are encrypted before storage.
// encryption_version=1 indicates encrypted tokens, version=0 indicates plaintext.
func UpsertOAuthTokenForChannel(ctx context.Context, dbx *sql.DB, provider, channel, access, refresh string, expiry time.Time, raw string, scope string) error {
	enc, err := getEncryptor()
	if err != nil {
		return fmt.Errorf("get encryptor: %w", err)
	}

	// Determine encryption version and encrypt tokens if encryption is enabled
	encVersion := 0
	encKeyID := ""
	accessToStore := access
	refreshToStore := refresh

	if enc != nil {
		// Encryption is enabled
		encVersion = 1
		encKeyID = "default" // Could be enhanced to support multiple keys

		// Encrypt access token
		if access != "" {
			encAccess, err := crypto.EncryptString(enc, access)
			if err != nil {
				return fmt.Errorf("encrypt access token: %w", err)
			}
			accessToStore = encAccess
		}

		// Encrypt refresh token
		if refresh != "" {
			encRefresh, err := crypto.EncryptString(enc, refresh)
			if err != nil {
				return fmt.Errorf("encrypt refresh token: %w", err)
			}
			refreshToStore = encRefresh
		}
	}

	q := `INSERT INTO oauth_tokens(provider, channel, access_token, refresh_token, expires_at, scope, encryption_version, encryption_key_id, updated_at)
		  VALUES($1,$2,$3,$4,$5,$6,$7,$8,NOW())
		  ON CONFLICT(provider, channel) DO UPDATE SET 
		    access_token=EXCLUDED.access_token, 
		    refresh_token=EXCLUDED.refresh_token, 
		    expires_at=EXCLUDED.expires_at, 
		    scope=EXCLUDED.scope,
		    encryption_version=EXCLUDED.encryption_version,
		    encryption_key_id=EXCLUDED.encryption_key_id,
		    updated_at=NOW()`
	_, err = dbx.ExecContext(ctx, q, provider, channel, accessToStore, refreshToStore, expiry, scope, encVersion, encKeyID)
	return err
}

// GetOAuthToken retrieves a stored token row; returns zero values if not found.
// Uses default channel (empty string) for backward compatibility.
// Automatically decrypts tokens if encryption_version=1 and encryption is configured.
// Supports backward compatibility: reads plaintext tokens (version=0) without decryption.
func GetOAuthToken(ctx context.Context, dbx *sql.DB, provider string) (access, refresh string, expiry time.Time, scope string, err error) {
	return GetOAuthTokenForChannel(ctx, dbx, provider, "")
}

// GetOAuthTokenForChannel retrieves a stored token row for a specific channel; returns zero values if not found.
// Automatically decrypts tokens if encryption_version=1 and encryption is configured.
// Supports backward compatibility: reads plaintext tokens (version=0) without decryption.
func GetOAuthTokenForChannel(ctx context.Context, dbx *sql.DB, provider, channel string) (access, refresh string, expiry time.Time, scope string, err error) {
	var encVersion int
	var encKeyID sql.NullString

	row := dbx.QueryRowContext(ctx,
		`SELECT access_token, refresh_token, expires_at, scope, COALESCE(encryption_version, 0), encryption_key_id 
		 FROM oauth_tokens WHERE provider = $1 AND channel = $2`, provider, channel)

	err = row.Scan(&access, &refresh, &expiry, &scope, &encVersion, &encKeyID)
	if err == sql.ErrNoRows {
		return "", "", time.Time{}, "", nil
	}
	if err != nil {
		return "", "", time.Time{}, "", err
	}

	// If encryption_version is 1, decrypt the tokens
	if encVersion == 1 {
		enc, encErr := getEncryptor()
		if encErr != nil {
			return "", "", time.Time{}, "", fmt.Errorf("get encryptor for decryption: %w", encErr)
		}
		if enc == nil {
			return "", "", time.Time{}, "", fmt.Errorf("token is encrypted but ENCRYPTION_KEY not configured")
		}

		// Decrypt access token
		if access != "" {
			decAccess, decErr := crypto.DecryptString(enc, access)
			if decErr != nil {
				return "", "", time.Time{}, "", fmt.Errorf("decrypt access token: %w", decErr)
			}
			access = decAccess
		}

		// Decrypt refresh token
		if refresh != "" {
			decRefresh, decErr := crypto.DecryptString(enc, refresh)
			if decErr != nil {
				return "", "", time.Time{}, "", fmt.Errorf("decrypt refresh token: %w", decErr)
			}
			refresh = decRefresh
		}
	}

	return access, refresh, expiry, scope, nil
}

// TokenStoreAdapter implements youtubeapi.TokenStore and reuses the table structure here.
type TokenStoreAdapter struct{ DB *sql.DB }

func (t *TokenStoreAdapter) UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error {
	return UpsertOAuthToken(ctx, t.DB, provider, accessToken, refreshToken, expiry, raw, "")
}

func (t *TokenStoreAdapter) GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error) {
	access, refresh, exp, scope, err := GetOAuthToken(ctx, t.DB, provider)
	return access, refresh, exp, scope, err
}
