package db

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// setupTestDB creates a test database connection and runs migrations for encryption tests
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
	ctx := context.Background()
	if err := Migrate(ctx, database); err != nil {
		database.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database
}

// TestEncryptedTokens tests the full encryption/decryption flow with database operations
func TestEncryptedTokens(t *testing.T) {
	// Generate a test encryption key
	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo=" // base64 encoded "test-encryption-key-32-bytes\n"

	// Save original ENCRYPTION_KEY and restore after test
	origKey := os.Getenv("ENCRYPTION_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("ENCRYPTION_KEY", origKey)
		} else {
			os.Unsetenv("ENCRYPTION_KEY")
		}
		// Reset encryptor for other tests
		encryptorOnce = sync.Once{}
		encryptor = nil
		errEncryptor = nil
	}()

	// Set encryption key for this test
	os.Setenv("ENCRYPTION_KEY", testKey)

	// Reset the encryptor to pick up new key
	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil

	db := setupTestDB(t)
	ctx := context.Background()

	provider := "test-encrypted-provider"
	accessToken := "test-access-token-12345"
	refreshToken := "test-refresh-token-67890"
	expiry := time.Now().Add(1 * time.Hour)
	scope := "test:scope1 test:scope2"

	// Test 1: Insert encrypted token
	err := UpsertOAuthToken(ctx, db, provider, accessToken, refreshToken, expiry, "", scope)
	if err != nil {
		t.Fatalf("UpsertOAuthToken() error = %v", err)
	}

	// Test 2: Verify token is encrypted in database (ciphertext != plaintext)
	var storedAccess, storedRefresh string
	var encVersion int
	err = db.QueryRow(`SELECT access_token, refresh_token, encryption_version FROM oauth_tokens WHERE provider=$1`, provider).
		Scan(&storedAccess, &storedRefresh, &encVersion)
	if err != nil {
		t.Fatalf("Failed to query stored token: %v", err)
	}

	if encVersion != 1 {
		t.Errorf("encryption_version = %d, want 1 (encrypted)", encVersion)
	}

	if storedAccess == accessToken {
		t.Errorf("access_token stored in plaintext, should be encrypted")
	}

	if storedRefresh == refreshToken {
		t.Errorf("refresh_token stored in plaintext, should be encrypted")
	}

	// Test 3: Retrieve and verify decryption works
	retrievedAccess, retrievedRefresh, retrievedExpiry, retrievedScope, err := GetOAuthToken(ctx, db, provider)
	if err != nil {
		t.Fatalf("GetOAuthToken() error = %v", err)
	}

	if retrievedAccess != accessToken {
		t.Errorf("retrieved access_token = %q, want %q", retrievedAccess, accessToken)
	}

	if retrievedRefresh != refreshToken {
		t.Errorf("retrieved refresh_token = %q, want %q", retrievedRefresh, refreshToken)
	}

	if retrievedScope != scope {
		t.Errorf("retrieved scope = %q, want %q", retrievedScope, scope)
	}

	if retrievedExpiry.Sub(expiry).Abs() > time.Second {
		t.Errorf("expiry mismatch: got %v, want %v", retrievedExpiry, expiry)
	}

	// Test 4: Update encrypted token
	newAccessToken := "new-access-token-99999"
	newRefreshToken := "new-refresh-token-88888"
	newExpiry := time.Now().Add(2 * time.Hour)
	newScope := "test:scope3"

	err = UpsertOAuthToken(ctx, db, provider, newAccessToken, newRefreshToken, newExpiry, "", newScope)
	if err != nil {
		t.Fatalf("UpsertOAuthToken() update error = %v", err)
	}

	// Verify updated values
	retrievedAccess, retrievedRefresh, retrievedExpiry, retrievedScope, err = GetOAuthToken(ctx, db, provider)
	if err != nil {
		t.Fatalf("GetOAuthToken() after update error = %v", err)
	}

	if retrievedAccess != newAccessToken {
		t.Errorf("updated access_token = %q, want %q", retrievedAccess, newAccessToken)
	}

	if retrievedRefresh != newRefreshToken {
		t.Errorf("updated refresh_token = %q, want %q", retrievedRefresh, newRefreshToken)
	}

	if retrievedScope != newScope {
		t.Errorf("updated scope = %q, want %q", retrievedScope, newScope)
	}
}

// TestPlaintextTokenCompatibility tests that plaintext tokens (encryption_version=0) can still be read
func TestPlaintextTokenCompatibility(t *testing.T) {
	// Ensure no encryption key is set for this test
	origKey := os.Getenv("ENCRYPTION_KEY")
	os.Unsetenv("ENCRYPTION_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("ENCRYPTION_KEY", origKey)
		}
		// Reset encryptor
		encryptorOnce = sync.Once{}
		encryptor = nil
		errEncryptor = nil
	}()

	// Reset encryptor
	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil

	db := setupTestDB(t)
	ctx := context.Background()

	provider := "test-plaintext-provider"
	accessToken := "plaintext-access-token"
	refreshToken := "plaintext-refresh-token"
	expiry := time.Now().Add(1 * time.Hour)
	scope := "plaintext:scope"

	// Insert token without encryption
	err := UpsertOAuthToken(ctx, db, provider, accessToken, refreshToken, expiry, "", scope)
	if err != nil {
		t.Fatalf("UpsertOAuthToken() error = %v", err)
	}

	// Verify token is stored in plaintext
	var storedAccess string
	var encVersion int
	err = db.QueryRow(`SELECT access_token, encryption_version FROM oauth_tokens WHERE provider=$1`, provider).
		Scan(&storedAccess, &encVersion)
	if err != nil {
		t.Fatalf("Failed to query stored token: %v", err)
	}

	if encVersion != 0 {
		t.Errorf("encryption_version = %d, want 0 (plaintext)", encVersion)
	}

	if storedAccess != accessToken {
		t.Errorf("stored access_token = %q, want %q (plaintext)", storedAccess, accessToken)
	}

	// Retrieve and verify plaintext token can be read
	retrievedAccess, retrievedRefresh, _, retrievedScope, err := GetOAuthToken(ctx, db, provider)
	if err != nil {
		t.Fatalf("GetOAuthToken() error = %v", err)
	}

	if retrievedAccess != accessToken {
		t.Errorf("retrieved access_token = %q, want %q", retrievedAccess, accessToken)
	}

	if retrievedRefresh != refreshToken {
		t.Errorf("retrieved refresh_token = %q, want %q", retrievedRefresh, refreshToken)
	}

	if retrievedScope != scope {
		t.Errorf("retrieved scope = %q, want %q", retrievedScope, scope)
	}
}

// TestEncryptionMigration tests migrating from plaintext to encrypted tokens
func TestEncryptionMigration(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := "test-migration-provider"
	accessToken := "migration-access-token"
	refreshToken := "migration-refresh-token"
	expiry := time.Now().Add(1 * time.Hour)
	scope := "migration:scope"

	// Step 1: Insert plaintext token (no encryption key)
	origKey := os.Getenv("ENCRYPTION_KEY")
	os.Unsetenv("ENCRYPTION_KEY")
	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil

	err := UpsertOAuthToken(ctx, db, provider, accessToken, refreshToken, expiry, "", scope)
	if err != nil {
		t.Fatalf("UpsertOAuthToken() plaintext error = %v", err)
	}

	// Verify plaintext storage
	var encVersion1 int
	err = db.QueryRow(`SELECT encryption_version FROM oauth_tokens WHERE provider=$1`, provider).Scan(&encVersion1)
	if err != nil {
		t.Fatalf("Failed to query encryption_version: %v", err)
	}
	if encVersion1 != 0 {
		t.Errorf("Initial encryption_version = %d, want 0", encVersion1)
	}

	// Step 2: Enable encryption and re-upsert (simulates migration on next token refresh)
	testKey := "dGVzdC1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcwo="
	os.Setenv("ENCRYPTION_KEY", testKey)
	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil

	// Simulate token refresh - write same token back with encryption
	err = UpsertOAuthToken(ctx, db, provider, accessToken, refreshToken, expiry, "", scope)
	if err != nil {
		t.Fatalf("UpsertOAuthToken() encrypted error = %v", err)
	}

	// Verify encrypted storage
	var encVersion2 int
	var storedAccess string
	err = db.QueryRow(`SELECT encryption_version, access_token FROM oauth_tokens WHERE provider=$1`, provider).
		Scan(&encVersion2, &storedAccess)
	if err != nil {
		t.Fatalf("Failed to query after migration: %v", err)
	}

	if encVersion2 != 1 {
		t.Errorf("After migration encryption_version = %d, want 1", encVersion2)
	}

	if storedAccess == accessToken {
		t.Errorf("After migration, token should be encrypted but is plaintext")
	}

	// Step 3: Verify retrieval still works
	retrievedAccess, retrievedRefresh, _, retrievedScope, err := GetOAuthToken(ctx, db, provider)
	if err != nil {
		t.Fatalf("GetOAuthToken() after migration error = %v", err)
	}

	if retrievedAccess != accessToken {
		t.Errorf("After migration retrieved access_token = %q, want %q", retrievedAccess, accessToken)
	}

	if retrievedRefresh != refreshToken {
		t.Errorf("After migration retrieved refresh_token = %q, want %q", retrievedRefresh, refreshToken)
	}

	if retrievedScope != scope {
		t.Errorf("After migration retrieved scope = %q, want %q", retrievedScope, scope)
	}

	// Cleanup
	if origKey != "" {
		os.Setenv("ENCRYPTION_KEY", origKey)
	} else {
		os.Unsetenv("ENCRYPTION_KEY")
	}
	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil
}

// TestEncryptionKeyNotSet verifies warning when encryption key is not configured
func TestEncryptionKeyNotSet(t *testing.T) {
	origKey := os.Getenv("ENCRYPTION_KEY")
	os.Unsetenv("ENCRYPTION_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("ENCRYPTION_KEY", origKey)
		}
		encryptorOnce = sync.Once{}
		encryptor = nil
		errEncryptor = nil
	}()

	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil

	enc, err := getEncryptor()
	if err != nil {
		t.Errorf("getEncryptor() should not error when key not set, got: %v", err)
	}
	if enc != nil {
		t.Errorf("getEncryptor() should return nil when key not set")
	}
}

// TestInvalidEncryptionKey tests handling of invalid encryption keys
func TestInvalidEncryptionKey(t *testing.T) {
	origKey := os.Getenv("ENCRYPTION_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("ENCRYPTION_KEY", origKey)
		} else {
			os.Unsetenv("ENCRYPTION_KEY")
		}
		encryptorOnce = sync.Once{}
		encryptor = nil
		errEncryptor = nil
	}()

	// Test invalid base64
	os.Setenv("ENCRYPTION_KEY", "not-valid-base64!@#")
	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil

	_, err := getEncryptor()
	if err == nil {
		t.Errorf("getEncryptor() with invalid base64 should return error")
	}

	// Test wrong key length
	os.Setenv("ENCRYPTION_KEY", "dGVzdAo=") // too short
	encryptorOnce = sync.Once{}
	encryptor = nil
	errEncryptor = nil

	_, err = getEncryptor()
	if err == nil {
		t.Errorf("getEncryptor() with wrong key length should return error")
	}
}
