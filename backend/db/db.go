package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func Connect() (*sql.DB, error) {
	return sql.Open("sqlite3", "vodtender.db")
}

func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS vods (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			twitch_vod_id TEXT UNIQUE,
			title TEXT,
			date DATETIME,
			processed BOOLEAN DEFAULT 0,
			youtube_url TEXT
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
			FOREIGN KEY(vod_id) REFERENCES vods(twitch_vod_id)
		);
	`)
	return err
}
