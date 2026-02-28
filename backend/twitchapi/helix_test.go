package twitchapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHelixClient_GetUserID(t *testing.T) {
	tests := []struct {
		response    interface{}
		name        string
		login       string
		wantUserID  string
		errContains string
		statusCode  int
		wantErr     bool
	}{
		{
			name:  "successful user lookup",
			login: "testuser",
			response: map[string]interface{}{
				"data": []map[string]string{
					{"id": "12345", "login": "testuser"},
				},
			},
			statusCode: http.StatusOK,
			wantUserID: "12345",
			wantErr:    false,
		},
		{
			name:  "user not found",
			login: "nonexistent",
			response: map[string]interface{}{
				"data": []map[string]string{},
			},
			statusCode:  http.StatusOK,
			wantErr:     true,
			errContains: "user not found",
		},
		{
			name:        "empty login",
			login:       "",
			wantErr:     true,
			errContains: "login empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify headers
				if r.Header.Get("Client-Id") != "test-client-id" {
					t.Errorf("missing or wrong Client-Id header")
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("missing or wrong Authorization header")
				}

				// Verify query params
				if tt.login != "" && r.URL.Query().Get("login") != tt.login {
					t.Errorf("login query param = %s, want %s", r.URL.Query().Get("login"), tt.login)
				}

				w.WriteHeader(tt.statusCode)
				if tt.response != nil {
					json.NewEncoder(w).Encode(tt.response)
				}
			}))
			defer server.Close()

			// Create client with mock token source
			ts := &TokenSource{
				ClientID:     "test-client-id",
				ClientSecret: "test-secret",
			}
			// Pre-seed the token to avoid OAuth calls
			ts.token = "test-token"
			ts.expiresAt = time.Now().Add(1 * time.Hour)

			client := &HelixClient{
				AppTokenSource: ts,
				ClientID:       "test-client-id",
				HTTPClient: &http.Client{
					Transport: &rewriteTransport{
						Transport: http.DefaultTransport,
						host:      server.URL,
					},
				},
			}

			userID, err := client.GetUserID(context.Background(), tt.login)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetUserID() error = nil, want error containing %q", tt.errContains)
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("GetUserID() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("GetUserID() unexpected error = %v", err)
				return
			}

			if userID != tt.wantUserID {
				t.Errorf("GetUserID() = %s, want %s", userID, tt.wantUserID)
			}
		})
	}
}

func TestHelixClient_ListVideos(t *testing.T) {
	tests := []struct {
		response    interface{}
		name        string
		userID      string
		after       string
		wantCursor  string
		errContains string
		first       int
		wantVideos  int
		wantErr     bool
	}{
		{
			name:   "successful video list",
			userID: "12345",
			after:  "",
			first:  20,
			response: map[string]interface{}{
				"data": []map[string]string{
					{
						"id":         "v123",
						"title":      "Test Video 1",
						"duration":   "1h30m45s",
						"created_at": "2024-01-01T10:00:00Z",
					},
					{
						"id":         "v124",
						"title":      "Test Video 2",
						"duration":   "45m30s",
						"created_at": "2024-01-01T09:00:00Z",
					},
				},
				"pagination": map[string]string{
					"cursor": "next-cursor-123",
				},
			},
			wantVideos: 2,
			wantCursor: "next-cursor-123",
			wantErr:    false,
		},
		{
			name:   "empty result",
			userID: "12345",
			first:  20,
			response: map[string]interface{}{
				"data":       []map[string]string{},
				"pagination": map[string]string{},
			},
			wantVideos: 0,
			wantCursor: "",
			wantErr:    false,
		},
		{
			name:        "empty userID",
			userID:      "",
			wantErr:     true,
			errContains: "userID empty",
		},
		{
			name:   "with pagination cursor",
			userID: "12345",
			after:  "cursor-abc",
			first:  50,
			response: map[string]interface{}{
				"data": []map[string]string{
					{
						"id":         "v125",
						"title":      "Test Video 3",
						"duration":   "2h",
						"created_at": "2024-01-01T08:00:00Z",
					},
				},
				"pagination": map[string]string{},
			},
			wantVideos: 1,
			wantCursor: "",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify headers
				if r.Header.Get("Client-Id") != "test-client-id" {
					t.Errorf("missing or wrong Client-Id header")
				}

				// Verify query params
				if tt.userID != "" {
					if r.URL.Query().Get("user_id") != tt.userID {
						t.Errorf("user_id = %s, want %s", r.URL.Query().Get("user_id"), tt.userID)
					}
					if r.URL.Query().Get("type") != "archive" {
						t.Errorf("type = %s, want archive", r.URL.Query().Get("type"))
					}
				}
				if tt.after != "" && r.URL.Query().Get("after") != tt.after {
					t.Errorf("after = %s, want %s", r.URL.Query().Get("after"), tt.after)
				}

				w.WriteHeader(http.StatusOK)
				if tt.response != nil {
					json.NewEncoder(w).Encode(tt.response)
				}
			}))
			defer server.Close()

			ts := &TokenSource{
				ClientID:     "test-client-id",
				ClientSecret: "test-secret",
			}
			ts.token = "test-token"
			ts.expiresAt = time.Now().Add(1 * time.Hour)

			client := &HelixClient{
				AppTokenSource: ts,
				ClientID:       "test-client-id",
				HTTPClient: &http.Client{
					Transport: &rewriteTransport{
						Transport: http.DefaultTransport,
						host:      server.URL,
					},
				},
			}

			videos, cursor, err := client.ListVideos(context.Background(), tt.userID, tt.after, tt.first)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ListVideos() error = nil, want error containing %q", tt.errContains)
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ListVideos() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ListVideos() unexpected error = %v", err)
				return
			}

			if len(videos) != tt.wantVideos {
				t.Errorf("ListVideos() returned %d videos, want %d", len(videos), tt.wantVideos)
			}

			if cursor != tt.wantCursor {
				t.Errorf("ListVideos() cursor = %s, want %s", cursor, tt.wantCursor)
			}

			// Verify video structure
			if len(videos) > 0 {
				v := videos[0]
				if v.ID == "" {
					t.Error("video ID is empty")
				}
				if v.Title == "" {
					t.Error("video title is empty")
				}
			}
		})
	}
}

func TestHelixClient_DefaultFirst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := r.URL.Query().Get("first")
		if first != "20" {
			t.Errorf("first = %s, want 20 (default)", first)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data":       []map[string]string{},
			"pagination": map[string]string{},
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
	}
	ts.token = "test-token"
	ts.expiresAt = time.Now().Add(1 * time.Hour)

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{
				Transport: http.DefaultTransport,
				host:      server.URL,
			},
		},
	}

	// Call with first = 0 should default to 20
	_, _, err := client.ListVideos(context.Background(), "12345", "", 0)
	if err != nil {
		t.Errorf("ListVideos() error = %v", err)
	}
}

// TestHelixClient_ListVideosEmptyPages tests handling of empty pages
func TestHelixClient_ListVideosEmptyPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return empty data array with no cursor
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data":       []map[string]string{},
			"pagination": map[string]string{},
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
	}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{
				Transport: http.DefaultTransport,
				host:      server.URL,
			},
		},
	}

	videos, cursor, err := client.ListVideos(context.Background(), "12345", "", 20)
	if err != nil {
		t.Fatalf("ListVideos() error = %v", err)
	}

	if len(videos) != 0 {
		t.Errorf("Expected 0 videos from empty page, got %d", len(videos))
	}

	if cursor != "" {
		t.Errorf("Expected empty cursor from empty page, got %q", cursor)
	}
}

// TestHelixClient_ListVideosPaginationCursors tests pagination with multiple pages
func TestHelixClient_ListVideosPaginationCursors(t *testing.T) {
	requestCount := 0
	cursorsReceived := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		afterCursor := r.URL.Query().Get("after")
		cursorsReceived = append(cursorsReceived, afterCursor)

		w.WriteHeader(http.StatusOK)

		// Page 1: return videos with cursor
		if afterCursor == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id":         "v1",
						"title":      "Video 1",
						"duration":   "1h",
						"created_at": "2024-01-01T10:00:00Z",
					},
					{
						"id":         "v2",
						"title":      "Video 2",
						"duration":   "45m",
						"created_at": "2024-01-01T09:00:00Z",
					},
				},
				"pagination": map[string]string{
					"cursor": "cursor-page2",
				},
			})
			return
		}

		// Page 2: return videos with cursor
		if afterCursor == "cursor-page2" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id":         "v3",
						"title":      "Video 3",
						"duration":   "30m",
						"created_at": "2024-01-01T08:00:00Z",
					},
				},
				"pagination": map[string]string{
					"cursor": "cursor-page3",
				},
			})
			return
		}

		// Page 3: return videos with no cursor (end)
		if afterCursor == "cursor-page3" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id":         "v4",
						"title":      "Video 4",
						"duration":   "15m",
						"created_at": "2024-01-01T07:00:00Z",
					},
				},
				"pagination": map[string]string{},
			})
			return
		}
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
	}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{
				Transport: http.DefaultTransport,
				host:      server.URL,
			},
		},
	}

	ctx := context.Background()

	// Fetch page 1
	videos1, cursor1, err := client.ListVideos(ctx, "12345", "", 20)
	if err != nil {
		t.Fatalf("ListVideos() page 1 error = %v", err)
	}
	if len(videos1) != 2 {
		t.Errorf("Expected 2 videos on page 1, got %d", len(videos1))
	}
	if cursor1 != "cursor-page2" {
		t.Errorf("Expected cursor 'cursor-page2' on page 1, got %q", cursor1)
	}

	// Fetch page 2
	videos2, cursor2, err := client.ListVideos(ctx, "12345", cursor1, 20)
	if err != nil {
		t.Fatalf("ListVideos() page 2 error = %v", err)
	}
	if len(videos2) != 1 {
		t.Errorf("Expected 1 video on page 2, got %d", len(videos2))
	}
	if cursor2 != "cursor-page3" {
		t.Errorf("Expected cursor 'cursor-page3' on page 2, got %q", cursor2)
	}

	// Fetch page 3 (final page)
	videos3, cursor3, err := client.ListVideos(ctx, "12345", cursor2, 20)
	if err != nil {
		t.Fatalf("ListVideos() page 3 error = %v", err)
	}
	if len(videos3) != 1 {
		t.Errorf("Expected 1 video on page 3, got %d", len(videos3))
	}
	if cursor3 != "" {
		t.Errorf("Expected empty cursor on final page, got %q", cursor3)
	}

	// Verify cursors were sent correctly
	expectedCursors := []string{"", "cursor-page2", "cursor-page3"}
	if len(cursorsReceived) != len(expectedCursors) {
		t.Errorf("Expected %d requests, got %d", len(expectedCursors), len(cursorsReceived))
	}
	for i, expected := range expectedCursors {
		if i < len(cursorsReceived) && cursorsReceived[i] != expected {
			t.Errorf("Request %d: expected cursor %q, got %q", i+1, expected, cursorsReceived[i])
		}
	}
}

// TestHelixClient_ListVideos429RateLimiting verifies retry behavior on 429 responses.
func TestHelixClient_ListVideos429RateLimiting(t *testing.T) {
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++

		// First attempt: return 429
		if attemptCount == 1 {
			w.Header().Set("Retry-After", "0")
			w.Header().Set("Ratelimit-Remaining", "0")
			w.Header().Set("Ratelimit-Reset", fmt.Sprintf("%d", time.Now().Add(1*time.Second).Unix()))
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Too Many Requests",
				"status":  429,
				"message": "Rate limit exceeded",
			})
			return
		}

		// Subsequent attempts: succeed
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":         "v1",
					"title":      "Rate Limited Video",
					"duration":   "1h",
					"created_at": "2024-01-01T10:00:00Z",
				},
			},
			"pagination": map[string]string{},
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
	}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{
				Transport: http.DefaultTransport,
				host:      server.URL,
			},
		},
	}

	videos, _, err := client.ListVideos(context.Background(), "12345", "", 20)
	if err != nil {
		t.Fatalf("ListVideos() unexpected error after 429 retry = %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("expected 1 video after retry, got %d", len(videos))
	}
	if attemptCount != 2 {
		t.Fatalf("expected 2 attempts (429 + success), got %d", attemptCount)
	}
}

func TestHelixClient_GetUserID401RefreshRetry(t *testing.T) {
	userAttempts := 0
	tokenRequests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			tokenRequests++
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fresh-token",
				"token_type":   "bearer",
				"expires_in":   3600,
			})
			return
		case "/helix/users":
			userAttempts++
			if userAttempts == 1 {
				if got := r.Header.Get("Authorization"); got != "Bearer stale-token" {
					t.Fatalf("first attempt auth = %q, want stale token", got)
				}
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Unauthorized", "status": 401})
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer fresh-token" {
				t.Fatalf("second attempt auth = %q, want refreshed token", got)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{{"id": "u-123"}},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rewrite := &http.Client{
		Transport: &rewriteTransport{
			Transport: http.DefaultTransport,
			host:      server.URL,
		},
	}

	ts := &TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		HTTPClient:   rewrite,
	}
	ts.SetToken("stale-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient:     rewrite,
	}

	userID, err := client.GetUserID(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("GetUserID() unexpected error = %v", err)
	}
	if userID != "u-123" {
		t.Fatalf("GetUserID() = %q, want u-123", userID)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected exactly one token refresh request, got %d", tokenRequests)
	}
	if userAttempts != 2 {
		t.Fatalf("expected two /helix/users attempts, got %d", userAttempts)
	}
}

func TestHelixClient_GetUserID401RefreshRetryOnFinalAttempt(t *testing.T) {
	userAttempts := 0
	tokenRequests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			tokenRequests++
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fresh-token",
				"token_type":   "bearer",
				"expires_in":   3600,
			})
			return
		case "/helix/users":
			userAttempts++
			if userAttempts < helixMaxRetries {
				// Serve 5xx to exhaust all-but-last retry slots using the stale token.
				if got := r.Header.Get("Authorization"); got != "Bearer stale-token" {
					t.Errorf("attempt %d auth = %q, want stale token", userAttempts, got)
				}
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "temporary error", "status": 500})
				return
			} else if userAttempts == helixMaxRetries {
				// Final retry with stale token should return 401 to trigger refresh.
				if got := r.Header.Get("Authorization"); got != "Bearer stale-token" {
					t.Errorf("final stale attempt auth = %q, want stale token", got)
				}
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Unauthorized", "status": 401})
				return
			}
			// Post-refresh attempt must use the freshly-obtained token.
			if got := r.Header.Get("Authorization"); got != "Bearer fresh-token" {
				t.Errorf("post-refresh attempt auth = %q, want fresh token", got)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{{"id": "u-456"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rewrite := &http.Client{
		Transport: &rewriteTransport{
			Transport: http.DefaultTransport,
			host:      server.URL,
		},
	}

	ts := &TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		HTTPClient:   rewrite,
	}
	ts.SetToken("stale-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient:     rewrite,
	}

	userID, err := client.GetUserID(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("GetUserID() unexpected error = %v", err)
	}
	if userID != "u-456" {
		t.Fatalf("GetUserID() = %q, want u-456", userID)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected exactly one token refresh, got %d", tokenRequests)
	}
	// helixMaxRetries attempts with stale token (incl. the final 401) + 1 with fresh token.
	expectedAttempts := helixMaxRetries + 1
	if userAttempts != expectedAttempts {
		t.Fatalf("expected %d /helix/users attempts, got %d", expectedAttempts, userAttempts)
	}
}

func TestHelixClient_GetStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/helix/streams" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("user_login"); got != "livechannel" {
			t.Fatalf("user_login=%q want livechannel", got)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]string{{
				"title":      "Live Now",
				"started_at": "2024-10-15T14:30:00Z",
			}},
		})
	}))
	defer server.Close()

	ts := &TokenSource{ClientID: "test-client-id", ClientSecret: "test-secret"}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{Transport: &rewriteTransport{
			Transport: http.DefaultTransport,
			host:      server.URL,
		}},
	}

	streams, err := client.GetStreams(context.Background(), "livechannel")
	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	if streams[0].Title != "Live Now" {
		t.Fatalf("stream title=%q want Live Now", streams[0].Title)
	}
}

func TestHelixClient_ListVideos5xxRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad gateway"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]string{{
				"id":         "v-retry",
				"title":      "Recovered",
				"duration":   "1h",
				"created_at": "2024-01-01T10:00:00Z",
			}},
			"pagination": map[string]string{},
		})
	}))
	defer server.Close()

	ts := &TokenSource{ClientID: "test-client-id", ClientSecret: "test-secret"}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{Transport: &rewriteTransport{
			Transport: http.DefaultTransport,
			host:      server.URL,
		}},
	}

	videos, _, err := client.ListVideos(context.Background(), "12345", "", 20)
	if err != nil {
		t.Fatalf("ListVideos() unexpected error after 5xx retry = %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(videos))
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts (5xx + success), got %d", attempts)
	}
}

// TestHelixClient_ListVideosNoCursorOnLastPage ensures empty cursor on final page
func TestHelixClient_ListVideosNoCursorOnLastPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return videos but no cursor (last page)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":         "v1",
					"title":      "Last Video",
					"duration":   "1h",
					"created_at": "2024-01-01T10:00:00Z",
				},
			},
			"pagination": map[string]string{}, // No cursor
		})
	}))
	defer server.Close()

	ts := &TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
	}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{
				Transport: http.DefaultTransport,
				host:      server.URL,
			},
		},
	}

	videos, cursor, err := client.ListVideos(context.Background(), "12345", "", 20)
	if err != nil {
		t.Fatalf("ListVideos() error = %v", err)
	}

	if len(videos) != 1 {
		t.Errorf("Expected 1 video, got %d", len(videos))
	}

	if cursor != "" {
		t.Errorf("Expected empty cursor on last page, got %q", cursor)
	}
}

// rewriteTransport rewrites all requests to use the test server
type rewriteTransport struct {
	Transport http.RoundTripper
	host      string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite URL to point to test server
	req.URL.Scheme = "http"
	// Parse the test server URL and use its host
	if t.host != "" {
		// Strip the scheme from host
		host := t.host
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimPrefix(host, "https://")
		req.URL.Host = host
	}
	return t.Transport.RoundTrip(req)
}
