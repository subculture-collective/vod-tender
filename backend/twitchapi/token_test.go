package twitchapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTokenSource_GetCached(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token-123",
			"expires_in":   3600,
			"token_type":   "bearer",
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		HTTPClient: &http.Client{
			Transport: &tokenTransport{host: server.URL},
		},
	}

	ctx := context.Background()

	// First call should fetch token
	token1, err := ts.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if token1 != "test-token-123" {
		t.Errorf("Get() = %s, want test-token-123", token1)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call should use cached token
	token2, err := ts.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if token2 != token1 {
		t.Errorf("cached token = %s, want %s", token2, token1)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 API call (cached), got %d", callCount)
	}
}

func TestTokenSource_GetRefreshExpired(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		token := "test-token-1"
		if callCount > 1 {
			token = "test-token-2"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": token,
			"expires_in":   1, // Expires in 1 second
			"token_type":   "bearer",
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		HTTPClient: &http.Client{
			Transport: &tokenTransport{host: server.URL},
		},
	}

	ctx := context.Background()

	// First call
	token1, err := ts.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if token1 != "test-token-1" {
		t.Errorf("Get() = %s, want test-token-1", token1)
	}

	// Wait for token to expire (plus buffer)
	time.Sleep(2 * time.Second)

	// Second call should refresh
	token2, err := ts.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if token2 != "test-token-2" {
		t.Errorf("Get() = %s, want test-token-2 (refreshed)", token2)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (initial + refresh), got %d", callCount)
	}
}

func TestTokenSource_GetMissingCredentials(t *testing.T) {
	ts := &TokenSource{
		ClientID:     "",
		ClientSecret: "",
	}

	_, err := ts.Get(context.Background())
	if err == nil {
		t.Error("Get() with missing credentials should return error")
	}
	if !strings.Contains(err.Error(), "missing client id/secret") {
		t.Errorf("Get() error = %v, want error about missing credentials", err)
	}
}

func TestTokenSource_GetServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "bad-client",
		ClientSecret: "bad-secret",
		HTTPClient: &http.Client{
			Transport: &tokenTransport{host: server.URL},
		},
	}

	_, err := ts.Get(context.Background())
	if err == nil {
		t.Error("Get() with server error should return error")
	}
}

func TestTokenSource_GetEmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "",
			"expires_in":   3600,
			"token_type":   "bearer",
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		HTTPClient: &http.Client{
			Transport: &tokenTransport{host: server.URL},
		},
	}

	_, err := ts.Get(context.Background())
	if err == nil {
		t.Error("Get() with empty access_token should return error")
	}
	if !strings.Contains(err.Error(), "empty access_token") {
		t.Errorf("Get() error = %v, want error about empty access_token", err)
	}
}

func TestTokenSource_ConcurrentAccess(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token",
			"expires_in":   3600,
			"token_type":   "bearer",
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		HTTPClient: &http.Client{
			Transport: &tokenTransport{host: server.URL},
		},
	}

	ctx := context.Background()

	// Launch multiple concurrent Get calls
	results := make(chan string, 5)
	errors := make(chan error, 5)

	for i := 0; i < 5; i++ {
		go func() {
			token, err := ts.Get(ctx)
			if err != nil {
				errors <- err
			} else {
				results <- token
			}
		}()
	}

	// Collect results
	for i := 0; i < 5; i++ {
		select {
		case err := <-errors:
			t.Errorf("Get() error = %v", err)
		case token := <-results:
			if token != "test-token" {
				t.Errorf("Get() = %s, want test-token", token)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for concurrent Gets")
		}
	}

	// Should only call API once despite concurrent requests
	// (some may race but should be minimal)
	if callCount > 2 {
		t.Errorf("expected at most 2 API calls with concurrent access, got %d", callCount)
	}
}

// tokenTransport is a custom transport for redirecting token requests
type tokenTransport struct {
	host string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite URL to point to test server
	req.URL.Scheme = "http"
	if t.host != "" {
		host := t.host
		if len(host) > 7 && host[:7] == "http://" {
			host = host[7:]
		}
		req.URL.Host = host
	}
	return http.DefaultTransport.RoundTrip(req)
}
