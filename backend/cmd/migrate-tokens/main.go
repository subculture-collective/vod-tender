// Package main provides a CLI tool to migrate OAuth tokens from plaintext to encrypted storage.
//
// This tool encrypts all tokens where encryption_version=0 (plaintext) to version=1 (AES-256-GCM encrypted).
// It requires ENCRYPTION_KEY environment variable to be set.
//
// Usage:
//   migrate-tokens [--dry-run] [--channel CHANNEL]
//
// Flags:
//   --dry-run: Show what would be migrated without making changes
//   --channel: Migrate tokens for specific channel only (default: all channels)
//
// Environment Variables:
//   DB_DSN: Database connection string (required)
//   ENCRYPTION_KEY: Base64-encoded 32-byte encryption key (required)
//
// Example:
//   export DB_DSN="postgres://vod:vod@localhost:5432/vod?sslmode=disable"
//   export ENCRYPTION_KEY="$(openssl rand -base64 32)"
//   ./migrate-tokens --dry-run
//   ./migrate-tokens
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/onnwee/vod-tender/backend/crypto"
)

// TokenRow represents an OAuth token row from the database
type TokenRow struct {
	Provider          string
	Channel           string
	AccessToken       string
	RefreshToken      string
	ExpiresAt         time.Time
	Scope             string
	EncryptionVersion int
	EncryptionKeyID   sql.NullString
}

func main() {
	// Parse command-line flags
	dryRun := flag.Bool("dry-run", false, "Show what would be migrated without making changes")
	channel := flag.String("channel", "", "Migrate tokens for specific channel only (default: all channels)")
	flag.Parse()

	// Setup structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Validate environment variables
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		slog.Error("DB_DSN environment variable is required")
		os.Exit(1)
	}

	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		slog.Error("ENCRYPTION_KEY environment variable is required for migration")
		os.Exit(1)
	}

	// Initialize encryptor
	encryptor, err := crypto.NewAESEncryptor(encryptionKey)
	if err != nil {
		slog.Error("failed to initialize encryptor", slog.Any("error", err))
		os.Exit(1)
	}

	// Connect to database
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		slog.Error("failed to connect to database", slog.Any("error", err))
		os.Exit(1)
	}
	defer database.Close()

	// Verify connection
	ctx := context.Background()
	if err := database.PingContext(ctx); err != nil {
		slog.Error("failed to ping database", slog.Any("error", err))
		os.Exit(1)
	}

	// Run migration
	if err := migrateTokens(ctx, database, encryptor, *dryRun, *channel); err != nil {
		slog.Error("migration failed", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("migration completed successfully")
}

// migrateTokens encrypts all plaintext tokens (encryption_version=0) in the database
func migrateTokens(ctx context.Context, database *sql.DB, encryptor crypto.Encryptor, dryRun bool, channelFilter string) error {
	// Query plaintext tokens
	query := `
		SELECT provider, channel, access_token, refresh_token, expires_at, scope, 
		       encryption_version, encryption_key_id
		FROM oauth_tokens
		WHERE encryption_version = 0
	`
	args := []interface{}{}

	// Add channel filter if specified
	if channelFilter != "" {
		query += " AND channel = $1"
		args = append(args, channelFilter)
	}

	query += " ORDER BY provider, channel"

	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to query plaintext tokens: %w", err)
	}
	defer rows.Close()

	// Collect tokens to migrate
	var tokens []TokenRow
	for rows.Next() {
		var token TokenRow
		if err := rows.Scan(
			&token.Provider,
			&token.Channel,
			&token.AccessToken,
			&token.RefreshToken,
			&token.ExpiresAt,
			&token.Scope,
			&token.EncryptionVersion,
			&token.EncryptionKeyID,
		); err != nil {
			return fmt.Errorf("failed to scan token row: %w", err)
		}
		tokens = append(tokens, token)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating token rows: %w", err)
	}

	// Report findings
	if len(tokens) == 0 {
		slog.Info("no plaintext tokens found to migrate")
		return nil
	}

	slog.Info("found plaintext tokens to migrate",
		slog.Int("count", len(tokens)),
		slog.Bool("dry_run", dryRun))

	// Migrate each token
	migratedCount := 0
	errorCount := 0

	for i, token := range tokens {
		logger := slog.With(
			slog.String("provider", token.Provider),
			slog.String("channel", token.Channel),
			slog.Int("index", i+1),
			slog.Int("total", len(tokens)))

		if dryRun {
			logger.Info("would migrate token (dry-run)")
			migratedCount++
			continue
		}

		// Encrypt the tokens
		if err := migrateToken(ctx, database, encryptor, token); err != nil {
			logger.Error("failed to migrate token", slog.Any("error", err))
			errorCount++
			continue
		}

		logger.Info("migrated token successfully")
		migratedCount++
	}

	// Report summary
	slog.Info("migration summary",
		slog.Int("total", len(tokens)),
		slog.Int("migrated", migratedCount),
		slog.Int("errors", errorCount),
		slog.Bool("dry_run", dryRun))

	if errorCount > 0 {
		return fmt.Errorf("migration completed with %d errors", errorCount)
	}

	return nil
}

// migrateToken encrypts a single token and updates the database
func migrateToken(ctx context.Context, database *sql.DB, encryptor crypto.Encryptor, token TokenRow) error {
	// Start transaction for atomic update
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback on error is best effort

	// Encrypt access token
	var encryptedAccess string
	if token.AccessToken != "" {
		encryptedAccess, err = crypto.EncryptString(encryptor, token.AccessToken)
		if err != nil {
			return fmt.Errorf("encrypt access token: %w", err)
		}
	}

	// Encrypt refresh token
	var encryptedRefresh string
	if token.RefreshToken != "" {
		encryptedRefresh, err = crypto.EncryptString(encryptor, token.RefreshToken)
		if err != nil {
			return fmt.Errorf("encrypt refresh token: %w", err)
		}
	}

	// Update database with encrypted tokens
	updateQuery := `
		UPDATE oauth_tokens
		SET access_token = $1,
		    refresh_token = $2,
		    encryption_version = 1,
		    encryption_key_id = 'default',
		    updated_at = NOW()
		WHERE provider = $3 AND channel = $4 AND encryption_version = 0
	`

	result, err := tx.ExecContext(ctx, updateQuery,
		encryptedAccess,
		encryptedRefresh,
		token.Provider,
		token.Channel)
	if err != nil {
		return fmt.Errorf("update token: %w", err)
	}

	// Verify exactly one row was updated
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row updated, got %d (token may have been modified concurrently)", rowsAffected)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ValidateMigration queries the database and reports encryption status of all tokens
func ValidateMigration(ctx context.Context, database *sql.DB) error {
	query := `
		SELECT encryption_version, COUNT(*) as count
		FROM oauth_tokens
		GROUP BY encryption_version
		ORDER BY encryption_version
	`

	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query validation: %w", err)
	}
	defer rows.Close()

	slog.Info("token encryption status:")
	totalTokens := 0

	for rows.Next() {
		var version int
		var count int
		if err := rows.Scan(&version, &count); err != nil {
			return fmt.Errorf("scan validation row: %w", err)
		}

		var versionDesc string
		switch version {
		case 0:
			versionDesc = "plaintext"
		case 1:
			versionDesc = "encrypted (AES-256-GCM)"
		default:
			versionDesc = fmt.Sprintf("unknown version %d", version)
		}

		slog.Info("  version",
			slog.Int("encryption_version", version),
			slog.String("description", versionDesc),
			slog.Int("count", count))

		totalTokens += count
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("validation rows iteration: %w", err)
	}

	slog.Info("total tokens", slog.Int("count", totalTokens))
	return nil
}
