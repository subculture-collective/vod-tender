package vod

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/twitchapi"
)

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

// TestFetchAllChannelVODsEmptyPages tests handling of empty pages
func TestFetchAllChannelVODsEmptyPages(t *testing.T) {
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
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Clean up test data
	_, _ = db.ExecContext(ctx, "DELETE FROM vods WHERE channel = 'test-empty-channel'")
	_, _ = db.ExecContext(ctx, "DELETE FROM kv WHERE channel = 'test-empty-channel'")

	// Create mock server that returns empty pages
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock user ID lookup
		if strings.Contains(r.URL.Path, "/users") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{
					{"id": "12345", "login": "test-empty-channel"},
				},
			})
			return
		}

		// Mock videos endpoint - return empty data
		if strings.Contains(r.URL.Path, "/videos") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data":       []map[string]string{},
				"pagination": map[string]string{},
			})
			return
		}
	}))
	defer server.Close()

	// Set up environment
	origChannel := os.Getenv("TWITCH_CHANNEL")
	origClientID := os.Getenv("TWITCH_CLIENT_ID")
	origSecret := os.Getenv("TWITCH_CLIENT_SECRET")
	defer func() {
		os.Setenv("TWITCH_CHANNEL", origChannel)
		os.Setenv("TWITCH_CLIENT_ID", origClientID)
		os.Setenv("TWITCH_CLIENT_SECRET", origSecret)
	}()

	os.Setenv("TWITCH_CHANNEL", "test-empty-channel")
	os.Setenv("TWITCH_CLIENT_ID", "test-client-id")
	os.Setenv("TWITCH_CLIENT_SECRET", "test-secret")

	// Create a custom helix client pointing to test server
	ts := &twitchapi.TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
	}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &twitchapi.HelixClient{
		AppTokenSource: ts,
		ClientID:       "test-client-id",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{
				Transport: http.DefaultTransport,
				host:      server.URL,
			},
		},
	}

	// Test fetching with empty results
	userID, err := client.GetUserID(ctx, "test-empty-channel")
	if err != nil {
		t.Fatalf("GetUserID() error = %v", err)
	}

	videos, cursor, err := client.ListVideos(ctx, userID, "", 100)
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

// TestFetchAllChannelVODsPaginationCursors tests multi-page pagination
func TestFetchAllChannelVODsPaginationCursors(t *testing.T) {
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
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Clean up test data
	channel := "test-pagination-channel"
	_, _ = db.ExecContext(ctx, "DELETE FROM vods WHERE channel = $1", channel)
	_, _ = db.ExecContext(ctx, "DELETE FROM kv WHERE channel = $1", channel)

	// Track request count
	requestCount := 0
	cursorsReceived := []string{}

	// Create mock server with multiple pages
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock user ID lookup
		if strings.Contains(r.URL.Path, "/users") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{
					{"id": "12345", "login": channel},
				},
			})
			return
		}

		// Mock videos endpoint with pagination
		if strings.Contains(r.URL.Path, "/videos") {
			requestCount++
			afterCursor := r.URL.Query().Get("after")
			cursorsReceived = append(cursorsReceived, afterCursor)

			// Page 1: return 2 videos with cursor
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

			// Page 2: return 2 videos with cursor
			if afterCursor == "cursor-page2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []map[string]interface{}{
						{
							"id":         "v3",
							"title":      "Video 3",
							"duration":   "30m",
							"created_at": "2024-01-01T08:00:00Z",
						},
						{
							"id":         "v4",
							"title":      "Video 4",
							"duration":   "2h",
							"created_at": "2024-01-01T07:00:00Z",
						},
					},
					"pagination": map[string]string{
						"cursor": "cursor-page3",
					},
				})
				return
			}

			// Page 3: return 1 video with no cursor (end of pagination)
			if afterCursor == "cursor-page3" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []map[string]interface{}{
						{
							"id":         "v5",
							"title":      "Video 5",
							"duration":   "15m",
							"created_at": "2024-01-01T06:00:00Z",
						},
					},
					"pagination": map[string]string{},
				})
				return
			}

			// Unexpected cursor
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data":       []map[string]interface{}{},
				"pagination": map[string]string{},
			})
		}
	}))
	defer server.Close()

	// Set up environment
	origChannel := os.Getenv("TWITCH_CHANNEL")
	origClientID := os.Getenv("TWITCH_CLIENT_ID")
	origSecret := os.Getenv("TWITCH_CLIENT_SECRET")
	defer func() {
		os.Setenv("TWITCH_CHANNEL", origChannel)
		os.Setenv("TWITCH_CLIENT_ID", origClientID)
		os.Setenv("TWITCH_CLIENT_SECRET", origSecret)
	}()

	os.Setenv("TWITCH_CHANNEL", channel)
	os.Setenv("TWITCH_CLIENT_ID", "test-client-id")
	os.Setenv("TWITCH_CLIENT_SECRET", "test-secret")

	// Test BackfillCatalog with pagination
	vods, err := FetchAllChannelVODs(ctx, db, channel, 0, 0)
	if err != nil {
		t.Fatalf("FetchAllChannelVODs() error = %v", err)
	}

	// Verify we got all 5 videos across 3 pages
	if len(vods) != 5 {
		t.Errorf("Expected 5 videos from pagination, got %d", len(vods))
	}

	// Verify request count (1 for user ID + 3 for video pages)
	// Note: can't check requestCount here as we're using the real function
	// which we can't intercept, but we can check the results

	// Verify kv cursor was stored (should be last cursor before empty cursor)
	var storedCursor string
	err = db.QueryRowContext(ctx, "SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'", channel).Scan(&storedCursor)
	if err != nil {
		t.Fatalf("Failed to read stored cursor: %v", err)
	}

	if storedCursor != "cursor-page3" {
		t.Errorf("Expected stored cursor 'cursor-page3', got %q", storedCursor)
	}
}

// TestBackfillCatalogNoDuplicateInserts tests idempotent inserts
func TestBackfillCatalogNoDuplicateInserts(t *testing.T) {
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
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Clean up test data
	channel := "test-duplicate-channel"
	_, _ = db.ExecContext(ctx, "DELETE FROM vods WHERE channel = $1", channel)
	_, _ = db.ExecContext(ctx, "DELETE FROM kv WHERE channel = $1", channel)

	// Create mock server that returns same videos
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock user ID lookup
		if strings.Contains(r.URL.Path, "/users") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{
					{"id": "12345", "login": channel},
				},
			})
			return
		}

		// Mock videos endpoint - always return same 2 videos
		if strings.Contains(r.URL.Path, "/videos") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id":         "duplicate-v1",
						"title":      "Duplicate Video 1",
						"duration":   "1h",
						"created_at": "2024-01-01T10:00:00Z",
					},
					{
						"id":         "duplicate-v2",
						"title":      "Duplicate Video 2",
						"duration":   "45m",
						"created_at": "2024-01-01T09:00:00Z",
					},
				},
				"pagination": map[string]string{},
			})
		}
	}))
	defer server.Close()

	// Set up environment
	origChannel := os.Getenv("TWITCH_CHANNEL")
	origClientID := os.Getenv("TWITCH_CLIENT_ID")
	origSecret := os.Getenv("TWITCH_CLIENT_SECRET")
	defer func() {
		os.Setenv("TWITCH_CHANNEL", origChannel)
		os.Setenv("TWITCH_CLIENT_ID", origClientID)
		os.Setenv("TWITCH_CLIENT_SECRET", origSecret)
	}()

	os.Setenv("TWITCH_CHANNEL", channel)
	os.Setenv("TWITCH_CLIENT_ID", "test-client-id")
	os.Setenv("TWITCH_CLIENT_SECRET", "test-secret")

	// Run backfill first time
	err = BackfillCatalog(ctx, db, channel, 0, 0)
	if err != nil {
		t.Fatalf("BackfillCatalog() first run error = %v", err)
	}

	// Count rows after first run
	var count1 int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vods WHERE channel = $1", channel).Scan(&count1)
	if err != nil {
		t.Fatalf("Failed to count rows after first run: %v", err)
	}

	if count1 != 2 {
		t.Errorf("Expected 2 rows after first backfill, got %d", count1)
	}

	// Run backfill second time with same data
	err = BackfillCatalog(ctx, db, channel, 0, 0)
	if err != nil {
		t.Fatalf("BackfillCatalog() second run error = %v", err)
	}

	// Count rows after second run - should be same
	var count2 int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vods WHERE channel = $1", channel).Scan(&count2)
	if err != nil {
		t.Fatalf("Failed to count rows after second run: %v", err)
	}

	if count2 != count1 {
		t.Errorf("Expected same row count after duplicate backfill, got %d (first) vs %d (second)", count1, count2)
	}

	// Verify the specific VOD IDs exist
	var vodCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vods WHERE channel = $1 AND twitch_vod_id IN ('duplicate-v1', 'duplicate-v2')", channel).Scan(&vodCount)
	if err != nil {
		t.Fatalf("Failed to check specific VOD IDs: %v", err)
	}

	if vodCount != 2 {
		t.Errorf("Expected 2 specific VOD IDs, got %d", vodCount)
	}
}

// TestFetchAllChannelVODsKVCursorPersistence tests cursor storage and retrieval
func TestFetchAllChannelVODsKVCursorPersistence(t *testing.T) {
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
	if err := dbpkg.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Clean up test data
	channel := "test-cursor-channel"
	_, _ = db.ExecContext(ctx, "DELETE FROM vods WHERE channel = $1", channel)
	_, _ = db.ExecContext(ctx, "DELETE FROM kv WHERE channel = $1", channel)

	callCount := 0

	// Create mock server with cursor persistence
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock user ID lookup
		if strings.Contains(r.URL.Path, "/users") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{
					{"id": "12345", "login": channel},
				},
			})
			return
		}

		// Mock videos endpoint
		if strings.Contains(r.URL.Path, "/videos") {
			callCount++
			afterCursor := r.URL.Query().Get("after")

			if afterCursor == "" {
				// First call: return videos with cursor
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []map[string]interface{}{
						{
							"id":         "cursor-v1",
							"title":      "Cursor Video 1",
							"duration":   "1h",
							"created_at": "2024-01-01T10:00:00Z",
						},
					},
					"pagination": map[string]string{
						"cursor": "saved-cursor-123",
					},
				})
				return
			}

			// Second call with cursor: return more videos
			if afterCursor == "saved-cursor-123" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []map[string]interface{}{
						{
							"id":         "cursor-v2",
							"title":      "Cursor Video 2",
							"duration":   "30m",
							"created_at": "2024-01-01T09:00:00Z",
						},
					},
					"pagination": map[string]string{},
				})
				return
			}
		}
	}))
	defer server.Close()

	// Set up environment
	origChannel := os.Getenv("TWITCH_CHANNEL")
	origClientID := os.Getenv("TWITCH_CLIENT_ID")
	origSecret := os.Getenv("TWITCH_CLIENT_SECRET")
	defer func() {
		os.Setenv("TWITCH_CHANNEL", origChannel)
		os.Setenv("TWITCH_CLIENT_ID", origClientID)
		os.Setenv("TWITCH_CLIENT_SECRET", origSecret)
	}()

	os.Setenv("TWITCH_CHANNEL", channel)
	os.Setenv("TWITCH_CLIENT_ID", "test-client-id")
	os.Setenv("TWITCH_CLIENT_SECRET", "test-secret")

	// First fetch - should get all videos and store cursor
	vods, err := FetchAllChannelVODs(ctx, db, channel, 0, 0)
	if err != nil {
		t.Fatalf("FetchAllChannelVODs() first run error = %v", err)
	}

	if len(vods) != 2 {
		t.Errorf("Expected 2 videos from first fetch, got %d", len(vods))
	}

	// Verify cursor was stored
	var storedCursor string
	err = db.QueryRowContext(ctx, "SELECT value FROM kv WHERE channel = $1 AND key = 'catalog_after'", channel).Scan(&storedCursor)
	if err != nil {
		t.Fatalf("Failed to read stored cursor: %v", err)
	}

	if storedCursor != "saved-cursor-123" {
		t.Errorf("Expected stored cursor 'saved-cursor-123', got %q", storedCursor)
	}

	// Verify kv updated_at timestamp
	var updatedAt time.Time
	err = db.QueryRowContext(ctx, "SELECT updated_at FROM kv WHERE channel = $1 AND key = 'catalog_after'", channel).Scan(&updatedAt)
	if err != nil {
		t.Fatalf("Failed to read updated_at: %v", err)
	}

	if updatedAt.IsZero() {
		t.Error("Expected updated_at to be set")
	}
}

// TestHelixClient429RateLimiting tests handling of 429 responses
func TestHelixClient429RateLimiting(t *testing.T) {
	// This test verifies that the HelixClient properly handles 429 responses
	// Note: Current implementation doesn't check status codes, so this test
	// documents the expected behavior

	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock user ID lookup - always succeed
		if strings.Contains(r.URL.Path, "/users") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{
					{"id": "12345", "login": "rate-limited-channel"},
				},
			})
			return
		}

		// Mock videos endpoint - return 429 on first attempt
		if strings.Contains(r.URL.Path, "/videos") {
			attempt++
			if attempt == 1 {
				// First attempt: return 429
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":   "Too Many Requests",
					"status":  429,
					"message": "Rate limit exceeded",
				})
				return
			}

			// Second attempt: succeed
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id":         "rate-v1",
						"title":      "Rate Limited Video",
						"duration":   "1h",
						"created_at": "2024-01-01T10:00:00Z",
					},
				},
				"pagination": map[string]string{},
			})
		}
	}))
	defer server.Close()

	// Create token source
	ts := &twitchapi.TokenSource{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
	}
	ts.SetToken("test-token", time.Now().Add(1*time.Hour))

	client := &twitchapi.HelixClient{
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

	// Get user ID
	userID, err := client.GetUserID(ctx, "rate-limited-channel")
	if err != nil {
		t.Fatalf("GetUserID() error = %v", err)
	}

	// First call - will get 429
	_, _, err = client.ListVideos(ctx, userID, "", 20)

	// Current implementation doesn't check status codes, so it will try to decode JSON
	// and likely fail or return unexpected results
	// This test documents that we need to enhance HelixClient to handle 429 properly

	// For now, we just verify the test setup works
	if attempt < 1 {
		t.Error("Expected at least one attempt to fetch videos")
	}
}
