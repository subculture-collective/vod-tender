// Package db provides database connection helpers, schema migration, and small data access helpers.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx postgres driver registered as 'pgx'
)

// Connect opens a Postgres connection using DB_DSN (or a sane default when running in Docker compose).
func Connect() (*sql.DB, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" { dsn = "postgres://vod:vod@postgres:5432/vod?sslmode=disable" }
	return sql.Open("pgx", dsn)
}

// Migrate applies idempotent schema changes for all required tables and indices.
func Migrate(db *sql.DB) error { return migratePostgres(db) }

func migratePostgres(db *sql.DB) error {
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
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_twitch_vod_id ON vods(twitch_vod_id)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_processed ON vods(processed)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_date ON vods(date)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_vod_rel ON chat_messages(vod_id, rel_timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_vod_abs ON chat_messages(vod_id, abs_timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_vods_proc_pri_date ON vods(processed, priority, date)`,
	}
	for i, s := range stmts {
		if _, err := db.Exec(s); err != nil { return fmt.Errorf("postgres migrate step %d failed: %w", i, err) }
	}
	return nil
}

// UpsertOAuthToken stores or updates an OAuth token for a provider (e.g., twitch, youtube).
func UpsertOAuthToken(ctx context.Context, dbx *sql.DB, provider, access, refresh string, expiry time.Time, raw string, scope string) error {
	q := `INSERT INTO oauth_tokens(provider, access_token, refresh_token, expires_at, scope, updated_at)
		  VALUES($1,$2,$3,$4,$5,NOW())
		  ON CONFLICT(provider) DO UPDATE SET access_token=EXCLUDED.access_token, refresh_token=EXCLUDED.refresh_token, expires_at=EXCLUDED.expires_at, scope=EXCLUDED.scope, updated_at=NOW()`
	_, err := dbx.ExecContext(ctx, q, provider, access, refresh, expiry, scope)
	return err
}

// GetOAuthToken retrieves a stored token row; returns zero values if not found.
func GetOAuthToken(ctx context.Context, dbx *sql.DB, provider string) (access, refresh string, expiry time.Time, scope string, err error) {
	row := dbx.QueryRowContext(ctx, `SELECT access_token, refresh_token, expires_at, scope FROM oauth_tokens WHERE provider = $1`, provider)
	err = row.Scan(&access, &refresh, &expiry, &scope)
	if err == sql.ErrNoRows { return "", "", time.Time{}, "", nil }
	return
}

// TokenStoreAdapter implements youtubeapi.TokenStore and reuses the table structure here.
type TokenStoreAdapter struct { DB *sql.DB }

func (t *TokenStoreAdapter) UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error {
	return UpsertOAuthToken(ctx, t.DB, provider, accessToken, refreshToken, expiry, raw, "")
}

func (t *TokenStoreAdapter) GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error) {
	access, refresh, exp, scope, err := GetOAuthToken(ctx, t.DB, provider)
	return access, refresh, exp, scope, err
}
