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
