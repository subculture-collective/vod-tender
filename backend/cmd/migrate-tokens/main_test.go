package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/onnwee/vod-tender/backend/crypto"
)

// setupTestDB creates a test database connection for migration tests
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Ensure oauth_tokens table exists
	ctx := context.Background()
	_, err = database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS oauth_tokens (
			provider TEXT,
			channel TEXT,
			access_token TEXT,
			refresh_token TEXT,
			expires_at TIMESTAMPTZ,
			scope TEXT,
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			encryption_version INTEGER DEFAULT 0,
			encryption_key_id TEXT,
			PRIMARY KEY (provider, channel)
		)
	`)
	if err != nil {
		database.Close()
		t.Fatalf("failed to create oauth_tokens table: %v", err)
	}

	t.Cleanup(func() {
		// Clean up test data
		_, _ = database.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE provider LIKE 'test-%'`)
		database.Close()
	})

	return database
}

// TestMigrateTokens_DryRun tests migration in dry-run mode
func TestMigrateTokens_DryRun(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Generate test encryption key
	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo="
	encryptor, err := crypto.NewAESEncryptor(testKey)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Insert test plaintext token
	provider := "test-provider-dryrun"
	channel := ""
	accessToken := "test-access-token"
	refreshToken := "test-refresh-token"
	expiry := time.Now().Add(1 * time.Hour)
	scope := "test:scope"

	_, err = db.ExecContext(ctx,
		`INSERT INTO oauth_tokens (provider, channel, access_token, refresh_token, expires_at, scope, encryption_version)
		 VALUES ($1, $2, $3, $4, $5, $6, 0)`,
		provider, channel, accessToken, refreshToken, expiry, scope)
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	// Run migration in dry-run mode
	err = migrateTokens(ctx, db, encryptor, true, "")
	if err != nil {
		t.Fatalf("migrateTokens(dry-run) failed: %v", err)
	}

	// Verify token is still plaintext (not migrated)
	var storedAccess string
	var encVersion int
	err = db.QueryRowContext(ctx,
		`SELECT access_token, encryption_version FROM oauth_tokens WHERE provider = $1`,
		provider).Scan(&storedAccess, &encVersion)
	if err != nil {
		t.Fatalf("failed to query token: %v", err)
	}

	if encVersion != 0 {
		t.Errorf("dry-run should not change encryption_version, got %d", encVersion)
	}

	if storedAccess != accessToken {
		t.Errorf("dry-run should not change access_token, got %q, want %q", storedAccess, accessToken)
	}
}

// TestMigrateTokens_RealMigration tests actual token migration
func TestMigrateTokens_RealMigration(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Generate test encryption key
	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo="
	encryptor, err := crypto.NewAESEncryptor(testKey)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Insert test plaintext tokens
	tokens := []struct {
		provider     string
		channel      string
		accessToken  string
		refreshToken string
	}{
		{"test-provider-1", "", "access-token-1", "refresh-token-1"},
		{"test-provider-2", "channel-a", "access-token-2", "refresh-token-2"},
	}

	for _, token := range tokens {
		_, err = db.ExecContext(ctx,
			`INSERT INTO oauth_tokens (provider, channel, access_token, refresh_token, expires_at, scope, encryption_version)
			 VALUES ($1, $2, $3, $4, NOW() + INTERVAL '1 hour', 'test:scope', 0)
			 ON CONFLICT (provider, channel) DO UPDATE SET access_token = EXCLUDED.access_token`,
			token.provider, token.channel, token.accessToken, token.refreshToken)
		if err != nil {
			t.Fatalf("failed to insert test token: %v", err)
		}
	}

	// Run actual migration
	err = migrateTokens(ctx, db, encryptor, false, "")
	if err != nil {
		t.Fatalf("migrateTokens() failed: %v", err)
	}

	// Verify all tokens are now encrypted
	for _, token := range tokens {
		var storedAccess, storedRefresh string
		var encVersion int
		var encKeyID sql.NullString

		err = db.QueryRowContext(ctx,
			`SELECT access_token, refresh_token, encryption_version, encryption_key_id 
			 FROM oauth_tokens WHERE provider = $1 AND channel = $2`,
			token.provider, token.channel).Scan(&storedAccess, &storedRefresh, &encVersion, &encKeyID)
		if err != nil {
			t.Fatalf("failed to query migrated token: %v", err)
		}

		// Verify encryption_version is 1
		if encVersion != 1 {
			t.Errorf("expected encryption_version=1, got %d", encVersion)
		}

		// Verify encryption_key_id is set
		if !encKeyID.Valid || encKeyID.String != "default" {
			t.Errorf("expected encryption_key_id='default', got %v", encKeyID)
		}

		// Verify tokens are encrypted (different from plaintext)
		if storedAccess == token.accessToken {
			t.Errorf("access_token should be encrypted, still plaintext")
		}

		if storedRefresh == token.refreshToken {
			t.Errorf("refresh_token should be encrypted, still plaintext")
		}

		// Verify tokens can be decrypted correctly
		decryptedAccess, err := crypto.DecryptString(encryptor, storedAccess)
		if err != nil {
			t.Fatalf("failed to decrypt access_token: %v", err)
		}
		if decryptedAccess != token.accessToken {
			t.Errorf("decrypted access_token = %q, want %q", decryptedAccess, token.accessToken)
		}

		decryptedRefresh, err := crypto.DecryptString(encryptor, storedRefresh)
		if err != nil {
			t.Fatalf("failed to decrypt refresh_token: %v", err)
		}
		if decryptedRefresh != token.refreshToken {
			t.Errorf("decrypted refresh_token = %q, want %q", decryptedRefresh, token.refreshToken)
		}
	}
}

// TestMigrateTokens_ChannelFilter tests migration with channel filter
func TestMigrateTokens_ChannelFilter(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Generate test encryption key
	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo="
	encryptor, err := crypto.NewAESEncryptor(testKey)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Insert test tokens for different channels
	_, err = db.ExecContext(ctx,
		`INSERT INTO oauth_tokens (provider, channel, access_token, refresh_token, expires_at, scope, encryption_version)
		 VALUES 
		   ('test-provider-filter-1', 'channel-x', 'access-x', 'refresh-x', NOW() + INTERVAL '1 hour', 'test:scope', 0),
		   ('test-provider-filter-2', 'channel-y', 'access-y', 'refresh-y', NOW() + INTERVAL '1 hour', 'test:scope', 0)
		 ON CONFLICT (provider, channel) DO UPDATE SET access_token = EXCLUDED.access_token`)
	if err != nil {
		t.Fatalf("failed to insert test tokens: %v", err)
	}

	// Migrate only channel-x
	err = migrateTokens(ctx, db, encryptor, false, "channel-x")
	if err != nil {
		t.Fatalf("migrateTokens() with channel filter failed: %v", err)
	}

	// Verify channel-x is encrypted
	var encVersionX int
	err = db.QueryRowContext(ctx,
		`SELECT encryption_version FROM oauth_tokens WHERE provider = 'test-provider-filter-1'`).Scan(&encVersionX)
	if err != nil {
		t.Fatalf("failed to query channel-x: %v", err)
	}
	if encVersionX != 1 {
		t.Errorf("channel-x should be encrypted (version=1), got version=%d", encVersionX)
	}

	// Verify channel-y is still plaintext
	var encVersionY int
	err = db.QueryRowContext(ctx,
		`SELECT encryption_version FROM oauth_tokens WHERE provider = 'test-provider-filter-2'`).Scan(&encVersionY)
	if err != nil {
		t.Fatalf("failed to query channel-y: %v", err)
	}
	if encVersionY != 0 {
		t.Errorf("channel-y should still be plaintext (version=0), got version=%d", encVersionY)
	}
}

// TestMigrateTokens_NoTokens tests migration when no tokens exist
func TestMigrateTokens_NoTokens(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo="
	encryptor, err := crypto.NewAESEncryptor(testKey)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Run migration on empty table
	err = migrateTokens(ctx, db, encryptor, false, "")
	if err != nil {
		t.Fatalf("migrateTokens() on empty table should succeed, got error: %v", err)
	}
}

// TestMigrateTokens_Idempotent tests that migration can be run multiple times
func TestMigrateTokens_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo="
	encryptor, err := crypto.NewAESEncryptor(testKey)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Insert plaintext token
	provider := "test-provider-idempotent"
	_, err = db.ExecContext(ctx,
		`INSERT INTO oauth_tokens (provider, channel, access_token, refresh_token, expires_at, scope, encryption_version)
		 VALUES ($1, '', 'access-token', 'refresh-token', NOW() + INTERVAL '1 hour', 'test:scope', 0)
		 ON CONFLICT (provider, channel) DO UPDATE SET access_token = EXCLUDED.access_token`,
		provider)
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	// Run migration first time
	err = migrateTokens(ctx, db, encryptor, false, "")
	if err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	// Run migration second time (should be no-op)
	err = migrateTokens(ctx, db, encryptor, false, "")
	if err != nil {
		t.Fatalf("second migration failed: %v", err)
	}

	// Verify still encrypted (version=1)
	var encVersion int
	err = db.QueryRowContext(ctx,
		`SELECT encryption_version FROM oauth_tokens WHERE provider = $1`,
		provider).Scan(&encVersion)
	if err != nil {
		t.Fatalf("failed to query token: %v", err)
	}

	if encVersion != 1 {
		t.Errorf("expected encryption_version=1, got %d", encVersion)
	}
}

// TestMigrateToken_EmptyTokens tests migration of tokens with empty access/refresh tokens
func TestMigrateToken_EmptyTokens(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo="
	encryptor, err := crypto.NewAESEncryptor(testKey)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Insert token with empty access and refresh tokens
	provider := "test-provider-empty"
	_, err = db.ExecContext(ctx,
		`INSERT INTO oauth_tokens (provider, channel, access_token, refresh_token, expires_at, scope, encryption_version)
		 VALUES ($1, '', '', '', NOW() + INTERVAL '1 hour', 'test:scope', 0)
		 ON CONFLICT (provider, channel) DO UPDATE SET access_token = EXCLUDED.access_token`,
		provider)
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	// Run migration
	err = migrateTokens(ctx, db, encryptor, false, "")
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Verify encryption_version is updated even for empty tokens
	var encVersion int
	var storedAccess, storedRefresh string
	err = db.QueryRowContext(ctx,
		`SELECT access_token, refresh_token, encryption_version FROM oauth_tokens WHERE provider = $1`,
		provider).Scan(&storedAccess, &storedRefresh, &encVersion)
	if err != nil {
		t.Fatalf("failed to query token: %v", err)
	}

	if encVersion != 1 {
		t.Errorf("expected encryption_version=1, got %d", encVersion)
	}

	// Empty tokens should remain empty
	if storedAccess != "" {
		t.Errorf("expected empty access_token, got %q", storedAccess)
	}
	if storedRefresh != "" {
		t.Errorf("expected empty refresh_token, got %q", storedRefresh)
	}
}
