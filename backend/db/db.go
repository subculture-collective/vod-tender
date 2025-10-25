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
	encryptorErr  error
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
			encryptorErr = fmt.Errorf("failed to initialize encryption: %w", err)
			slog.Error("encryption initialization failed", slog.Any("error", encryptorErr), slog.String("component", "db_encryption"))
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
	if encryptorErr != nil {
		return nil, encryptorErr
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
func Migrate(ctx context.Context, db *sql.DB) error { return migratePostgres(ctx, db) }

func migratePostgres(ctx context.Context, db *sql.DB) error {
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
		`CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
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
	}
	for i, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("postgres migrate step %d failed: %w", i, err)
		}
	}
	return nil
}

// UpsertOAuthToken stores or updates an OAuth token for a provider (e.g., twitch, youtube).
// If encryption is enabled (ENCRYPTION_KEY set), tokens are encrypted before storage.
// encryption_version=1 indicates encrypted tokens, version=0 indicates plaintext.
func UpsertOAuthToken(ctx context.Context, dbx *sql.DB, provider, access, refresh string, expiry time.Time, raw string, scope string) error {
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

	q := `INSERT INTO oauth_tokens(provider, access_token, refresh_token, expires_at, scope, encryption_version, encryption_key_id, updated_at)
		  VALUES($1,$2,$3,$4,$5,$6,$7,NOW())
		  ON CONFLICT(provider) DO UPDATE SET 
		    access_token=EXCLUDED.access_token, 
		    refresh_token=EXCLUDED.refresh_token, 
		    expires_at=EXCLUDED.expires_at, 
		    scope=EXCLUDED.scope,
		    encryption_version=EXCLUDED.encryption_version,
		    encryption_key_id=EXCLUDED.encryption_key_id,
		    updated_at=NOW()`
	_, err = dbx.ExecContext(ctx, q, provider, accessToStore, refreshToStore, expiry, scope, encVersion, encKeyID)
	return err
}

// GetOAuthToken retrieves a stored token row; returns zero values if not found.
// Automatically decrypts tokens if encryption_version=1 and encryption is configured.
// Supports backward compatibility: reads plaintext tokens (version=0) without decryption.
func GetOAuthToken(ctx context.Context, dbx *sql.DB, provider string) (access, refresh string, expiry time.Time, scope string, err error) {
	var encVersion int
	var encKeyID sql.NullString

	row := dbx.QueryRowContext(ctx, 
		`SELECT access_token, refresh_token, expires_at, scope, COALESCE(encryption_version, 0), encryption_key_id 
		 FROM oauth_tokens WHERE provider = $1`, provider)
	
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
