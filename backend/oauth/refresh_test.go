package oauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/onnwee/vod-tender/backend/testutil"
)

func TestStartRefresherDefaults(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Setup: insert a token that doesn't need refresh yet
	futureExpiry := time.Now().Add(1 * time.Hour)
	_, err := db.Exec(`INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		"test-provider", "access123", "refresh456", futureExpiry, "scope1")
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	refreshCalled := false
	refreshFunc := func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		refreshCalled = true
		return "new-access", "new-refresh", time.Now().Add(2 * time.Hour), "scope1", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start refresher with very short interval
	StartRefresher(ctx, db, "test-provider", 50*time.Millisecond, 30*time.Minute, refreshFunc)

	// Wait for context to expire
	<-ctx.Done()

	// Token should not be refreshed because expiry is still far in the future
	if refreshCalled {
		t.Error("refresh should not have been called for token that expires in 1 hour with 30 min window")
	}
}

func TestStartRefresherWithinWindow(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Setup: insert a token that needs refresh (expires in 5 minutes, window is 15 minutes)
	soonExpiry := time.Now().Add(5 * time.Minute)
	_, err := db.Exec(`INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		"test-provider", "old-access", "old-refresh", soonExpiry, "scope1")
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	refreshCalled := false
	newExpiry := time.Now().Add(2 * time.Hour)
	refreshFunc := func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		if refreshToken != "old-refresh" {
			t.Errorf("refresh called with wrong token: got %s, want old-refresh", refreshToken)
		}
		refreshCalled = true
		return "new-access", "new-refresh", newExpiry, "scope2", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start refresher with short interval and wide window
	StartRefresher(ctx, db, "test-provider", 100*time.Millisecond, 15*time.Minute, refreshFunc)

	// Wait for at least one refresh cycle
	time.Sleep(300 * time.Millisecond)
	cancel()

	if !refreshCalled {
		t.Error("refresh should have been called for token expiring within window")
	}

	// Verify token was updated in database
	var access, refresh, scope string
	var expiry time.Time
	err = db.QueryRow(`SELECT access_token, refresh_token, expires_at, scope FROM oauth_tokens WHERE provider='test-provider'`).
		Scan(&access, &refresh, &expiry, &scope)
	if err != nil {
		t.Fatalf("failed to query updated token: %v", err)
	}

	if access != "new-access" {
		t.Errorf("access token not updated: got %s, want new-access", access)
	}
	if refresh != "new-refresh" {
		t.Errorf("refresh token not updated: got %s, want new-refresh", refresh)
	}
	if scope != "scope2" {
		t.Errorf("scope not updated: got %s, want scope2", scope)
	}
}

func TestStartRefresherRefreshError(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Setup: insert a token that needs refresh
	soonExpiry := time.Now().Add(5 * time.Minute)
	_, err := db.Exec(`INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		"test-provider", "old-access", "old-refresh", soonExpiry, "scope1")
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	refreshFunc := func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		return "", "", time.Time{}, "", errors.New("refresh failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	StartRefresher(ctx, db, "test-provider", 50*time.Millisecond, 15*time.Minute, refreshFunc)

	time.Sleep(200 * time.Millisecond)
	cancel()

	// Verify token was NOT updated (should remain old values)
	var access string
	err = db.QueryRow(`SELECT access_token FROM oauth_tokens WHERE provider='test-provider'`).Scan(&access)
	if err != nil {
		t.Fatalf("failed to query token: %v", err)
	}
	if access != "old-access" {
		t.Errorf("token should not have been updated on error, got %s", access)
	}
}

func TestStartRefresherNoRefreshToken(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Setup: insert a token without refresh token
	soonExpiry := time.Now().Add(5 * time.Minute)
	_, err := db.Exec(`INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		"test-provider", "access123", "", soonExpiry, "scope1")
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	refreshCalled := false
	refreshFunc := func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		refreshCalled = true
		return "new-access", "new-refresh", time.Now().Add(2 * time.Hour), "scope1", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	StartRefresher(ctx, db, "test-provider", 50*time.Millisecond, 15*time.Minute, refreshFunc)

	time.Sleep(150 * time.Millisecond)
	cancel()

	// Should not attempt refresh without refresh token
	if refreshCalled {
		t.Error("refresh should not be called when refresh_token is empty")
	}
}

func TestStartRefresherCancellation(t *testing.T) {
	db := testutil.SetupTestDB(t)

	refreshFunc := func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		return "access", "refresh", time.Now().Add(1 * time.Hour), "scope", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	
	// Start refresher
	StartRefresher(ctx, db, "test-provider", 1*time.Second, 15*time.Minute, refreshFunc)

	// Cancel immediately
	cancel()

	// Give it a moment to exit
	time.Sleep(50 * time.Millisecond)

	// If we get here without hanging, cancellation works
}

func TestStartRefresherPreservesRefreshToken(t *testing.T) {
	db := testutil.SetupTestDB(t)

	soonExpiry := time.Now().Add(5 * time.Minute)
	_, err := db.Exec(`INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		"test-provider", "old-access", "original-refresh", soonExpiry, "scope1")
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	// Refresh function returns empty refresh token (should preserve original)
	refreshFunc := func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		return "new-access", "", time.Now().Add(2 * time.Hour), "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	StartRefresher(ctx, db, "test-provider", 50*time.Millisecond, 15*time.Minute, refreshFunc)

	time.Sleep(200 * time.Millisecond)
	cancel()

	var refresh, scope string
	err = db.QueryRow(`SELECT refresh_token, scope FROM oauth_tokens WHERE provider='test-provider'`).
		Scan(&refresh, &scope)
	if err != nil {
		t.Fatalf("failed to query token: %v", err)
	}

	// Should preserve original refresh token and scope
	if refresh != "original-refresh" {
		t.Errorf("refresh token should be preserved, got %s, want original-refresh", refresh)
	}
	if scope != "scope1" {
		t.Errorf("scope should be preserved, got %s, want scope1", scope)
	}
}

// TestStartRefresherWithEncryption verifies that token refresh uses encryption helpers.
// This test works with or without ENCRYPTION_KEY - it verifies the integration with
// db.UpsertOAuthToken which handles encryption automatically when configured.
func TestStartRefresherWithEncryption(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Setup: insert a plaintext token that needs refresh (expires in 5 minutes)
	soonExpiry := time.Now().Add(5 * time.Minute)
	_, err := db.Exec(`INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope, encryption_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())`,
		"test-encrypted", "plaintext-access", "plaintext-refresh", soonExpiry, "test:scope", 0)
	if err != nil {
		t.Fatalf("failed to insert test token: %v", err)
	}

	newExpiry := time.Now().Add(2 * time.Hour)
	refreshFunc := func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		if refreshToken != "plaintext-refresh" {
			t.Errorf("refresh called with wrong token: got %s, want plaintext-refresh", refreshToken)
		}
		return "new-encrypted-access", "new-encrypted-refresh", newExpiry, "test:scope", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start refresher with short interval and wide window
	StartRefresher(ctx, db, "test-encrypted", 100*time.Millisecond, 15*time.Minute, refreshFunc)

	// Wait for at least one refresh cycle
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Verify token was updated
	var storedAccess, storedRefresh string
	var encVersion int
	err = db.QueryRow(`SELECT access_token, refresh_token, encryption_version FROM oauth_tokens WHERE provider=$1`, "test-encrypted").
		Scan(&storedAccess, &storedRefresh, &encVersion)
	if err != nil {
		t.Fatalf("failed to query updated token: %v", err)
	}

	// If ENCRYPTION_KEY is set, tokens are encrypted (encryption_version = 1)
	// If not set, tokens remain plaintext (encryption_version = 0)
	// The StartRefresher delegates to db.UpsertOAuthToken which handles encryption automatically
	t.Logf("Token stored with encryption_version=%d, access_token length=%d", encVersion, len(storedAccess))

	// Basic verification: token should have been updated (either plaintext or encrypted)
	if storedAccess == "plaintext-access" {
		t.Error("access token should have been updated after refresh")
	}
	if storedRefresh == "plaintext-refresh" {
		t.Error("refresh token should have been updated after refresh")
	}
}
