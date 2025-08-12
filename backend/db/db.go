package db

import (
	"context"
	"database/sql"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func Connect() (*sql.DB, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "vodtender.db"
	}
	return sql.Open("sqlite3", dsn)
}

func Migrate(db *sql.DB) error {
	// Phase 1: ensure core tables & basic indexes (exclude composite index that references possibly-missing column).
	_, err := db.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE IF NOT EXISTS vods (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			twitch_vod_id TEXT UNIQUE,
			title TEXT,
			date DATETIME,
			duration_seconds INTEGER,
			downloaded_path TEXT,
			download_state TEXT,
			download_retries INTEGER DEFAULT 0,
			download_bytes INTEGER DEFAULT 0,
			download_total INTEGER DEFAULT 0,
			progress_updated_at DATETIME,
			processed BOOLEAN DEFAULT 0,
			processing_error TEXT,
			youtube_url TEXT,
			priority INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS chat_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			vod_id TEXT NOT NULL,
			username TEXT,
			message TEXT,
			abs_timestamp DATETIME,
			rel_timestamp REAL,
			badges TEXT,
			emotes TEXT,
			color TEXT,
			reply_to_id TEXT,
			reply_to_username TEXT,
			reply_to_message TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(vod_id) REFERENCES vods(twitch_vod_id)
		);
		CREATE INDEX IF NOT EXISTS idx_vods_twitch_vod_id ON vods(twitch_vod_id);
		CREATE INDEX IF NOT EXISTS idx_vods_processed ON vods(processed);
		CREATE INDEX IF NOT EXISTS idx_vods_date ON vods(date);
		CREATE INDEX IF NOT EXISTS idx_chat_vod_rel ON chat_messages(vod_id, rel_timestamp);
		CREATE INDEX IF NOT EXISTS idx_chat_vod_abs ON chat_messages(vod_id, abs_timestamp);
		CREATE TABLE IF NOT EXISTS oauth_tokens (
			provider TEXT PRIMARY KEY,
			access_token TEXT,
			refresh_token TEXT,
			expires_at DATETIME,
			scope TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}
	// Attempt to add new columns for existing databases. Ignore failures.
	_, _ = db.Exec(`ALTER TABLE chat_messages ADD COLUMN reply_to_id TEXT`)
	_, _ = db.Exec(`ALTER TABLE chat_messages ADD COLUMN reply_to_username TEXT`)
	_, _ = db.Exec(`ALTER TABLE chat_messages ADD COLUMN reply_to_message TEXT`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_state TEXT`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_retries INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_bytes INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_total INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN progress_updated_at DATETIME`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN priority INTEGER DEFAULT 0`)

	// Phase 2: create composite index (processed, priority, date) after ensuring 'priority' column exists.
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_vods_proc_pri_date ON vods(processed, priority, date)`)
	return nil
}

// UpsertOAuthToken stores or updates an OAuth token.
func UpsertOAuthToken(ctx context.Context, db *sql.DB, provider, access, refresh string, expiry time.Time, raw string, scope string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO oauth_tokens(provider, access_token, refresh_token, expires_at, scope, updated_at) VALUES(?,?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(provider) DO UPDATE SET access_token=excluded.access_token, refresh_token=excluded.refresh_token, expires_at=excluded.expires_at, scope=excluded.scope, updated_at=CURRENT_TIMESTAMP`, provider, access, refresh, expiry, scope)
	return err
}

// GetOAuthToken retrieves a stored token row.
func GetOAuthToken(ctx context.Context, db *sql.DB, provider string) (access, refresh string, expiry time.Time, scope string, err error) {
	row := db.QueryRowContext(ctx, `SELECT access_token, refresh_token, expires_at, scope FROM oauth_tokens WHERE provider = ?`, provider)
	err = row.Scan(&access, &refresh, &expiry, &scope)
	if err == sql.ErrNoRows { return "", "", time.Time{}, "", nil }
	return
}

// TokenStoreAdapter implements youtubeapi.TokenStore
type TokenStoreAdapter struct { DB *sql.DB }

func (t *TokenStoreAdapter) UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error {
	return UpsertOAuthToken(ctx, t.DB, provider, accessToken, refreshToken, expiry, raw, "")
}

func (t *TokenStoreAdapter) GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error) {
	access, refresh, exp, scope, err := GetOAuthToken(ctx, t.DB, provider)
	return access, refresh, exp, scope, err
}
