package testutil

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/onnwee/vod-tender/backend/db"
)

// SetupTestDB creates a test database connection and runs migrations.
// It skips the test if TEST_PG_DSN environment variable is not set.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		database.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database
}
