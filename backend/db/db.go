package db

import (
	"database/sql"
	"os"

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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(vod_id) REFERENCES vods(twitch_vod_id)
		);
	CREATE INDEX IF NOT EXISTS idx_vods_twitch_vod_id ON vods(twitch_vod_id);
	CREATE INDEX IF NOT EXISTS idx_vods_processed ON vods(processed);
		CREATE INDEX IF NOT EXISTS idx_chat_vod_rel ON chat_messages(vod_id, rel_timestamp);
		CREATE INDEX IF NOT EXISTS idx_chat_vod_abs ON chat_messages(vod_id, abs_timestamp);
	`)
	if err != nil {
		return err
	}
	// Attempt to add new columns for existing databases. Ignore failures.
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_state TEXT`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_retries INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_bytes INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN download_total INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE vods ADD COLUMN progress_updated_at DATETIME`)
	return nil
}
