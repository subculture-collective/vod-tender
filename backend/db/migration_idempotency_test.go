package db

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMigrateIdempotency tests that running Migrate multiple times doesn't cause errors
// and produces the correct schema. This specifically tests the constraint recreation logic.
func TestMigrateIdempotency(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set; skipping idempotency test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()

	ctx := context.Background()

	// Run migration first time
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	// Verify oauth_tokens has correct primary key (provider, channel)
	verifyOAuthTokensPK := func(t *testing.T) {
		var keyColumns string
		err := db.QueryRowContext(ctx, `
			SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum::smallint))
			FROM   pg_index i
			JOIN   pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE  i.indrelid = 'oauth_tokens'::regclass
			AND    i.indisprimary
		`).Scan(&keyColumns)
		if err != nil {
			t.Fatalf("failed to query oauth_tokens primary key: %v", err)
		}
		if keyColumns != "provider,channel" {
			t.Errorf("oauth_tokens primary key = %s, want provider,channel", keyColumns)
		}
	}

	// Verify kv has correct primary key (channel, key)
	verifyKvPK := func(t *testing.T) {
		var keyColumns string
		err := db.QueryRowContext(ctx, `
			SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum::smallint))
			FROM   pg_index i
			JOIN   pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE  i.indrelid = 'kv'::regclass
			AND    i.indisprimary
		`).Scan(&keyColumns)
		if err != nil {
			t.Fatalf("failed to query kv primary key: %v", err)
		}
		if keyColumns != "channel,key" {
			t.Errorf("kv primary key = %s, want channel,key", keyColumns)
		}
	}

	verifyOAuthTokensPK(t)
	verifyKvPK(t)

	// Run migration second time - should be idempotent
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	// Verify constraints are still correct
	verifyOAuthTokensPK(t)
	verifyKvPK(t)

	// Run migration third time - should still be idempotent
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("third migrate: %v", err)
	}

	// Verify constraints are still correct
	verifyOAuthTokensPK(t)
	verifyKvPK(t)
}

// TestMigrateFromOldSchema tests upgrading from the old single-channel schema
// where oauth_tokens had PK (provider) and kv had PK (key)
func TestMigrateFromOldSchema(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set; skipping old schema upgrade test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()

	ctx := context.Background()

	// Create a fresh database state by dropping and recreating tables
	// to simulate old schema
	_, err = db.ExecContext(ctx, `DROP TABLE IF EXISTS chat_messages CASCADE`)
	if err != nil {
		t.Fatalf("drop chat_messages: %v", err)
	}
	_, err = db.ExecContext(ctx, `DROP TABLE IF EXISTS vods CASCADE`)
	if err != nil {
		t.Fatalf("drop vods: %v", err)
	}
	_, err = db.ExecContext(ctx, `DROP TABLE IF EXISTS oauth_tokens CASCADE`)
	if err != nil {
		t.Fatalf("drop oauth_tokens: %v", err)
	}
	_, err = db.ExecContext(ctx, `DROP TABLE IF EXISTS kv CASCADE`)
	if err != nil {
		t.Fatalf("drop kv: %v", err)
	}

	// Create old schema (without channel columns, old primary keys)
	oldSchemaStmts := []string{
		`CREATE TABLE vods (
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
		`CREATE TABLE chat_messages (
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
		`CREATE TABLE oauth_tokens (
			provider TEXT PRIMARY KEY,
			access_token TEXT,
			refresh_token TEXT,
			expires_at TIMESTAMPTZ,
			scope TEXT,
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			encryption_version INTEGER DEFAULT 0,
			encryption_key_id TEXT
		)`,
		`CREATE TABLE kv (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
	}

	for i, stmt := range oldSchemaStmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("old schema step %d failed: %v", i, err)
		}
	}

	// Insert some test data in old format
	_, err = db.ExecContext(ctx, `INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope) VALUES ('twitch', 'old_access', 'old_refresh', NOW() + INTERVAL '1 hour', 'scope1')`)
	if err != nil {
		t.Fatalf("insert old oauth token: %v", err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO kv (key, value) VALUES ('test_key', 'test_value')`)
	if err != nil {
		t.Fatalf("insert old kv: %v", err)
	}

	// Run migration - should upgrade schema
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate from old schema: %v", err)
	}

	// Verify oauth_tokens has new primary key and channel column
	var keyColumns string
	err = db.QueryRowContext(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum::smallint))
		FROM   pg_index i
		JOIN   pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE  i.indrelid = 'oauth_tokens'::regclass
		AND    i.indisprimary
	`).Scan(&keyColumns)
	if err != nil {
		t.Fatalf("failed to query oauth_tokens primary key after migration: %v", err)
	}
	if keyColumns != "provider,channel" {
		t.Errorf("after migration, oauth_tokens primary key = %s, want provider,channel", keyColumns)
	}

	// Verify old data is preserved (with default channel='')
	var access, channel string
	err = db.QueryRowContext(ctx, `SELECT access_token, channel FROM oauth_tokens WHERE provider='twitch'`).Scan(&access, &channel)
	if err != nil {
		t.Fatalf("failed to query old oauth token: %v", err)
	}
	if access != "old_access" {
		t.Errorf("access_token = %s, want old_access", access)
	}
	if channel != "" {
		t.Errorf("channel = %s, want empty string (default)", channel)
	}

	// Verify kv has new primary key
	err = db.QueryRowContext(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum::smallint))
		FROM   pg_index i
		JOIN   pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE  i.indrelid = 'kv'::regclass
		AND    i.indisprimary
	`).Scan(&keyColumns)
	if err != nil {
		t.Fatalf("failed to query kv primary key after migration: %v", err)
	}
	if keyColumns != "channel,key" {
		t.Errorf("after migration, kv primary key = %s, want channel,key", keyColumns)
	}

	// Verify old kv data is preserved
	var value string
	err = db.QueryRowContext(ctx, `SELECT value, channel FROM kv WHERE key='test_key'`).Scan(&value, &channel)
	if err != nil {
		t.Fatalf("failed to query old kv: %v", err)
	}
	if value != "test_value" {
		t.Errorf("value = %s, want test_value", value)
	}
	if channel != "" {
		t.Errorf("channel = %s, want empty string (default)", channel)
	}

	// Run migration again to ensure idempotency after upgrade
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate after upgrade: %v", err)
	}

	// Verify constraints are still correct
	err = db.QueryRowContext(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum::smallint))
		FROM   pg_index i
		JOIN   pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE  i.indrelid = 'oauth_tokens'::regclass
		AND    i.indisprimary
	`).Scan(&keyColumns)
	if err != nil {
		t.Fatalf("failed to query oauth_tokens primary key after second migrate: %v", err)
	}
	if keyColumns != "provider,channel" {
		t.Errorf("after second migrate, oauth_tokens primary key = %s, want provider,channel", keyColumns)
	}
}
