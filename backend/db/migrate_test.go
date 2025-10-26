package db

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestRunMigrations tests that migrations can be applied to an empty database
func TestRunMigrations(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set; skipping migration test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Clean up any existing schema
	ctx := context.Background()
	cleanDatabase(t, ctx, db)

	// Run migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	// Verify tables exist
	tables := []string{"vods", "chat_messages", "oauth_tokens", "kv"}
	for _, table := range tables {
		var exists bool
		err := db.QueryRow(`SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_name = $1
		)`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist after migration", table)
		}
	}

	// Verify migration version
	version, dirty, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() error = %v", err)
	}
	if dirty {
		t.Errorf("migration version is dirty")
	}
	if version < 1 {
		t.Errorf("migration version = %d, want >= 1", version)
	}
}

// TestMigrationsIdempotent tests that running migrations multiple times is safe
func TestMigrationsIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	cleanDatabase(t, ctx, db)

	// Run migrations first time
	if err := RunMigrations(db); err != nil {
		t.Fatalf("first RunMigrations() error = %v", err)
	}

	version1, dirty1, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() after first migration error = %v", err)
	}

	// Run migrations second time - should be no-op
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second RunMigrations() error = %v", err)
	}

	version2, dirty2, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() after second migration error = %v", err)
	}

	if version1 != version2 {
		t.Errorf("version changed: %d -> %d (should be stable)", version1, version2)
	}
	if dirty1 != dirty2 {
		t.Errorf("dirty state changed: %v -> %v", dirty1, dirty2)
	}
}

// TestMigrationUpDown tests forward and backward migration
func TestMigrationUpDown(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	cleanDatabase(t, ctx, db)

	// Apply all migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	// Verify tables exist
	var vodsExists bool
	err = db.QueryRow(`SELECT EXISTS (
		SELECT FROM information_schema.tables 
		WHERE table_name = 'vods'
	)`).Scan(&vodsExists)
	if err != nil {
		t.Fatalf("failed to check vods table: %v", err)
	}
	if !vodsExists {
		t.Fatal("vods table does not exist after up migration")
	}

	// Insert test data to verify it survives appropriate migrations
	_, err = db.ExecContext(ctx, `INSERT INTO vods (twitch_vod_id, title) VALUES ('test123', 'Test VOD')`)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Get current version
	versionBefore, _, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() before down error = %v", err)
	}

	// Roll back last migration
	if err := MigrateDown(db); err != nil {
		t.Fatalf("MigrateDown() error = %v", err)
	}

	// Verify version decreased
	versionAfter, dirty, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() after down error = %v", err)
	}
	if dirty {
		t.Errorf("migration is dirty after down")
	}
	if versionAfter >= versionBefore {
		t.Errorf("version did not decrease: %d -> %d", versionBefore, versionAfter)
	}

	// Verify core tables still exist (should not be dropped by rolling back index migration)
	err = db.QueryRow(`SELECT EXISTS (
		SELECT FROM information_schema.tables 
		WHERE table_name = 'vods'
	)`).Scan(&vodsExists)
	if err != nil {
		t.Fatalf("failed to check vods table after down: %v", err)
	}
	if !vodsExists {
		t.Error("vods table should still exist after rolling back last migration")
	}

	// Re-apply migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() after rollback error = %v", err)
	}

	// Verify we're back to the latest version
	versionFinal, dirty, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() after re-apply error = %v", err)
	}
	if dirty {
		t.Errorf("migration is dirty after re-apply")
	}
	if versionFinal != versionBefore {
		t.Errorf("version after re-apply = %d, want %d", versionFinal, versionBefore)
	}
}

// TestMigrationDownAll tests rolling back all migrations
func TestMigrationDownAll(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	cleanDatabase(t, ctx, db)

	// Apply all migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	// Get current version
	versionStart, _, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() error = %v", err)
	}

	// Roll back all migrations one by one
	for i := uint(0); i < versionStart; i++ {
		if err := MigrateDown(db); err != nil {
			t.Fatalf("MigrateDown() iteration %d error = %v", i, err)
		}
	}

	// Verify all tables are gone
	tables := []string{"vods", "chat_messages", "oauth_tokens", "kv"}
	for _, table := range tables {
		var exists bool
		err := db.QueryRow(`SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_name = $1
		)`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check table %s: %v", table, err)
		}
		if exists {
			t.Errorf("table %s still exists after rolling back all migrations", table)
		}
	}

	// Verify version is at 0 (no migrations)
	version, _, err := GetMigrationVersion(db)
	if err != nil {
		t.Fatalf("GetMigrationVersion() after down all error = %v", err)
	}
	if version != 0 {
		t.Errorf("version after rolling back all = %d, want 0", version)
	}
}

// TestMigrationWithData tests that migrations preserve existing data appropriately
func TestMigrationWithData(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	cleanDatabase(t, ctx, db)

	// Apply all migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	// Insert test data
	testVodID := "test_vod_123"
	testTitle := "Test VOD Title"
	_, err = db.ExecContext(ctx, `
		INSERT INTO vods (twitch_vod_id, title, channel, processed) 
		VALUES ($1, $2, 'default', false)
	`, testVodID, testTitle)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Verify data exists
	var title string
	err = db.QueryRowContext(ctx, `SELECT title FROM vods WHERE twitch_vod_id = $1`, testVodID).Scan(&title)
	if err != nil {
		t.Fatalf("failed to query test data: %v", err)
	}
	if title != testTitle {
		t.Errorf("title = %s, want %s", title, testTitle)
	}

	// Roll back last migration (performance indices)
	if err := MigrateDown(db); err != nil {
		t.Fatalf("MigrateDown() error = %v", err)
	}

	// Verify data still exists
	err = db.QueryRowContext(ctx, `SELECT title FROM vods WHERE twitch_vod_id = $1`, testVodID).Scan(&title)
	if err != nil {
		t.Fatalf("failed to query test data after rollback: %v", err)
	}
	if title != testTitle {
		t.Errorf("after rollback: title = %s, want %s", title, testTitle)
	}

	// Re-apply migration
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() after rollback error = %v", err)
	}

	// Verify data still exists
	err = db.QueryRowContext(ctx, `SELECT title FROM vods WHERE twitch_vod_id = $1`, testVodID).Scan(&title)
	if err != nil {
		t.Fatalf("failed to query test data after re-apply: %v", err)
	}
	if title != testTitle {
		t.Errorf("after re-apply: title = %s, want %s", title, testTitle)
	}
}

// cleanDatabase drops all tables and the schema_migrations table to start fresh
func cleanDatabase(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	statements := []string{
		`DROP TABLE IF EXISTS chat_messages CASCADE`,
		`DROP TABLE IF EXISTS vods CASCADE`,
		`DROP TABLE IF EXISTS oauth_tokens CASCADE`,
		`DROP TABLE IF EXISTS kv CASCADE`,
		`DROP TABLE IF EXISTS schema_migrations CASCADE`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Logf("warning: clean database statement failed (may be expected): %v", err)
		}
	}
}
