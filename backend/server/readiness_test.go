package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestReadyzReady(t *testing.T) {
	db := newTestDB(t)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	})

	// Insert a mock OAuth token to satisfy the credentials check
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at)
		VALUES ('twitch', 'test_access', 'test_refresh', NOW() + INTERVAL '1 hour')
	`)
	if err != nil {
		t.Fatalf("insert mock oauth token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	h := NewMux(db)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["status"] != "ready" {
		t.Fatalf("expected status=ready, got %q", resp["status"])
	}
}

func TestReadyzNotReadyCircuitOpen(t *testing.T) {
	db := newTestDB(t)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	})

	// Insert a mock OAuth token
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at)
		VALUES ('twitch', 'test_access', 'test_refresh', NOW() + INTERVAL '1 hour')
	`)
	if err != nil {
		t.Fatalf("insert mock oauth token: %v", err)
	}

	// Set circuit breaker to open state
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO kv (key, value, updated_at)
		VALUES ('circuit_state', 'open', NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		t.Fatalf("set circuit state: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	h := NewMux(db)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d, body=%s", rr.Code, rr.Body.String())
	}

	// Ensure Content-Type is set before status write path
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type=application/json, got %q", ct)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["status"] != "not_ready" {
		t.Fatalf("expected status=not_ready, got %q", resp["status"])
	}

	if resp["failed_check"] != "circuit_breaker" {
		t.Fatalf("expected failed_check=circuit_breaker, got %q", resp["failed_check"])
	}
}

func TestReadyzNotReadyMissingCredentials(t *testing.T) {
	db := newTestDB(t)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	})

	// Don't insert any OAuth tokens

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	h := NewMux(db)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d, body=%s", rr.Code, rr.Body.String())
	}

	// Ensure Content-Type is set before status write path
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type=application/json, got %q", ct)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["status"] != "not_ready" {
		t.Fatalf("expected status=not_ready, got %q", resp["status"])
	}

	if resp["failed_check"] != "credentials" {
		t.Fatalf("expected failed_check=credentials, got %q", resp["failed_check"])
	}
}
