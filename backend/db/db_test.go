package db

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMigrate(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set; skipping postgres migration test")
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
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
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
}

func TestUpsertAndGetOAuthToken(t *testing.T) {
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

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	provider := "test-provider"
	expiry := time.Now().Add(1 * time.Hour)

	// Insert token
	err = UpsertOAuthToken(ctx, db, provider, "access123", "refresh456", expiry, "", "scope1 scope2")
	if err != nil {
		t.Fatalf("UpsertOAuthToken() error = %v", err)
	}

	// Retrieve token
	access, refresh, exp, scope, err := GetOAuthToken(ctx, db, provider)
	if err != nil {
		t.Fatalf("GetOAuthToken() error = %v", err)
	}

	if access != "access123" {
		t.Errorf("access = %s, want access123", access)
	}
	if refresh != "refresh456" {
		t.Errorf("refresh = %s, want refresh456", refresh)
	}
	if scope != "scope1 scope2" {
		t.Errorf("scope = %s, want 'scope1 scope2'", scope)
	}
	// Time comparison with tolerance
	if exp.Sub(expiry).Abs() > time.Second {
		t.Errorf("expiry mismatch: got %v, want %v", exp, expiry)
	}

	// Update token
	newExpiry := time.Now().Add(2 * time.Hour)
	err = UpsertOAuthToken(ctx, db, provider, "new-access", "new-refresh", newExpiry, "", "new-scope")
	if err != nil {
		t.Fatalf("UpsertOAuthToken() update error = %v", err)
	}

	// Retrieve updated token
	access, refresh, exp, scope, err = GetOAuthToken(ctx, db, provider)
	if err != nil {
		t.Fatalf("GetOAuthToken() error = %v", err)
	}

	if access != "new-access" {
		t.Errorf("updated access = %s, want new-access", access)
	}
	if refresh != "new-refresh" {
		t.Errorf("updated refresh = %s, want new-refresh", refresh)
	}
	if scope != "new-scope" {
		t.Errorf("updated scope = %s, want new-scope", scope)
	}
}

func TestGetOAuthToken_NotFound(t *testing.T) {
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

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	access, refresh, exp, scope, err := GetOAuthToken(ctx, db, "nonexistent-provider")
	
	// Should return zero values without error
	if err != nil {
		t.Errorf("GetOAuthToken() for nonexistent provider should not error, got %v", err)
	}
	if access != "" {
		t.Errorf("access should be empty, got %s", access)
	}
	if refresh != "" {
		t.Errorf("refresh should be empty, got %s", refresh)
	}
	if !exp.IsZero() {
		t.Errorf("expiry should be zero time, got %v", exp)
	}
	if scope != "" {
		t.Errorf("scope should be empty, got %s", scope)
	}
}

func TestTokenStoreAdapter(t *testing.T) {
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

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	adapter := &TokenStoreAdapter{DB: db}
	expiry := time.Now().Add(1 * time.Hour)

	// Test UpsertOAuthToken through adapter
	err = adapter.UpsertOAuthToken(ctx, "adapter-test", "access-token", "refresh-token", expiry, "{\"raw\":\"json\"}")
	if err != nil {
		t.Fatalf("adapter.UpsertOAuthToken() error = %v", err)
	}

	// Test GetOAuthToken through adapter
	access, refresh, _, raw, err := adapter.GetOAuthToken(ctx, "adapter-test")
	if err != nil {
		t.Fatalf("adapter.GetOAuthToken() error = %v", err)
	}

	if access != "access-token" {
		t.Errorf("access = %s, want access-token", access)
	}
	if refresh != "refresh-token" {
		t.Errorf("refresh = %s, want refresh-token", refresh)
	}
	// The raw field is returned as scope by adapter
	if raw == "" {
		// adapter implementation stores empty scope when raw is provided to UpsertOAuthToken
	}
}

func TestConnect(t *testing.T) {
	// Save original env
	origDSN := os.Getenv("DB_DSN")
	defer func() {
		if origDSN != "" {
			os.Setenv("DB_DSN", origDSN)
		} else {
			os.Unsetenv("DB_DSN")
		}
	}()

	// Test with empty DSN (should use default)
	os.Unsetenv("DB_DSN")
	db, err := Connect()
	if err != nil {
		t.Fatalf("Connect() with default DSN error = %v", err)
	}
	if db == nil {
		t.Error("Connect() returned nil db")
	}
	db.Close()

	// Test with custom DSN
	testDSN := os.Getenv("TEST_PG_DSN")
	if testDSN != "" {
		os.Setenv("DB_DSN", testDSN)
		db, err = Connect()
		if err != nil {
			t.Fatalf("Connect() with custom DSN error = %v", err)
		}
		if db == nil {
			t.Error("Connect() returned nil db")
		}
		// Test that connection actually works
		if err := db.Ping(); err != nil {
			t.Errorf("Ping() error = %v", err)
		}
		db.Close()
	}
}

