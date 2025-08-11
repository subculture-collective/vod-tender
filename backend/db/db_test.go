package db

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrate(t *testing.T) {
    database, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }
    defer database.Close()

    if err := Migrate(database); err != nil {
        t.Fatalf("migrate: %v", err)
    }
}
