// Package db provides database connection helpers, schema migration, and small data access helpers.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// getMigrationsPath returns the path to the migrations directory.
// It looks for the migrations in several locations to handle different execution contexts:
// 1. db/migrations (when running from backend/)
// 2. migrations (when running from backend/db/)
// 3. backend/db/migrations (when running from repo root)
func getMigrationsPath() (string, error) {
	possiblePaths := []string{
		"db/migrations",
		"migrations",
		"backend/db/migrations",
		"./db/migrations",
		"./migrations",
		"./backend/db/migrations",
	}

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return "", fmt.Errorf("failed to get absolute path for %s: %w", path, err)
			}
			return "file://" + absPath, nil
		}
	}

	return "", fmt.Errorf("migrations directory not found in any of the expected locations: %v", possiblePaths)
}

// RunMigrations runs versioned database migrations using golang-migrate.
// Migrations are located in db/migrations/ directory.
// This function is idempotent and safe to run multiple times.
//
// Migration files follow the naming convention:
//   000001_description.up.sql   - applies the migration
//   000001_description.down.sql - reverts the migration
//
// Example usage:
//
//	db, err := sql.Open("pgx", dsn)
//	if err != nil {
//	    return err
//	}
//	if err := RunMigrations(db); err != nil {
//	    return err
//	}
func RunMigrations(db *sql.DB) error {
	migrationsPath, err := getMigrationsPath()
	if err != nil {
		return err
	}
	return RunMigrationsFromPath(db, migrationsPath)
}

// RunMigrationsFromPath runs migrations from a custom path.
// This is useful for testing with different migration directories.
func RunMigrationsFromPath(db *sql.DB, migrationsPath string) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	// Run all pending migrations
	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			// No new migrations to apply - this is not an error
			slog.Info("database schema is up to date", slog.String("component", "db_migrate"))
			return nil
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		// If we can't get version, it might be because there are no migrations
		// This is okay, just log and continue
		slog.Warn("could not determine migration version", slog.Any("error", err), slog.String("component", "db_migrate"))
		return nil
	}

	if dirty {
		return fmt.Errorf("database is in dirty state at version %d - manual intervention required", version)
	}

	slog.Info("migrations applied successfully",
		slog.Uint64("version", uint64(version)),
		slog.String("component", "db_migrate"))

	return nil
}

// MigrateDown rolls back the most recent migration.
// This should only be used in development or emergency rollback scenarios.
// WARNING: This may result in data loss depending on the migration.
func MigrateDown(db *sql.DB) error {
	migrationsPath, err := getMigrationsPath()
	if err != nil {
		return err
	}
	return MigrateDownFromPath(db, migrationsPath)
}

// MigrateDownFromPath rolls back the most recent migration from a custom path.
func MigrateDownFromPath(db *sql.DB, migrationsPath string) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	// Roll back one migration
	if err := m.Steps(-1); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			slog.Info("no migrations to roll back", slog.String("component", "db_migrate"))
			return nil
		}
		return fmt.Errorf("failed to roll back migration: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		// After rolling back all migrations, Version() returns an error
		// This is expected behavior
		slog.Info("rolled back to no migrations", slog.String("component", "db_migrate"))
		return nil
	}

	if dirty {
		return fmt.Errorf("database is in dirty state at version %d after rollback - manual intervention required", version)
	}

	slog.Info("migration rolled back successfully",
		slog.Uint64("version", uint64(version)),
		slog.String("component", "db_migrate"))

	return nil
}

// GetMigrationVersion returns the current migration version and dirty state.
func GetMigrationVersion(db *sql.DB) (version uint, dirty bool, err error) {
	migrationsPath, mErr := getMigrationsPath()
	if mErr != nil {
		return 0, false, mErr
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return 0, false, fmt.Errorf("failed to create postgres driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	v, d, err := m.Version()
	if err != nil {
		// If there are no migrations applied yet, this is expected
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to get migration version: %w", err)
	}

	return v, d, nil
}
