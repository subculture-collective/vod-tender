package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/onnwee/vod-tender/backend/testutil"
)

func TestCORS(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(context.Background(), db) // NewMux now includes CORS config internally

	req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	// OPTIONS should return 204 (NoContent)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Errorf("OPTIONS request status = %d, want %d or %d", resp.StatusCode, http.StatusNoContent, http.StatusOK)
	}

	// Check CORS headers
	headers := []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	}
	for _, h := range headers {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing CORS header: %s", h)
		}
	}
}

func TestHealthzEndpoint(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(context.Background(), db)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	// Just check we got a response
	if len(body) == 0 {
		t.Error("healthz returned empty response")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(context.Background(), db)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("metrics status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	// Should contain some metrics
	if len(body) == 0 {
		t.Error("metrics returned empty response")
	}
}

func TestConfigEndpoint(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(context.Background(), db)

	t.Setenv("TWITCH_CHANNEL", "test_channel")
	t.Setenv("TWITCH_VOD_ID", "test_vod")

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("config status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var cfg map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("failed to decode config response: %v", err)
	}

	if cfg["twitch_channel"] != "test_channel" {
		t.Errorf("twitch_channel = %v, want test_channel", cfg["twitch_channel"])
	}
}

func TestVodsEndpoint(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(context.Background(), db)

	// Insert test VOD
	_, err := db.Exec(`INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ('test123', 'Test VOD', NOW(), 3600, NOW())`)
	if err != nil {
		t.Fatalf("failed to insert test vod: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/vods", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("vods status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var vods []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&vods); err != nil {
		t.Fatalf("failed to decode vods response: %v", err)
	}

	if len(vods) == 0 {
		t.Error("expected at least one VOD in response")
	}

	// Check first VOD has expected fields
	if len(vods) > 0 {
		vod := vods[0]
		if vod["twitch_vod_id"] != "test123" {
			t.Errorf("vod twitch_vod_id = %v, want test123", vod["twitch_vod_id"])
		}
		if vod["title"] != "Test VOD" {
			t.Errorf("vod title = %v, want Test VOD", vod["title"])
		}
	}
}

func TestStatusEndpoint(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(context.Background(), db)

	// Insert sample EMA values for backward compatibility test
	_, _ = db.Exec(`INSERT INTO kv (key, value, updated_at) VALUES 
		('avg_download_ms', '300000', NOW()),
		('avg_upload_ms', '120000', NOW()),
		('avg_total_ms', '420000', NOW())
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}

	// Check expected fields
	expectedFields := []string{"active_downloads", "queued", "total_vods", "circuit_state"}
	for _, field := range expectedFields {
		if _, ok := status[field]; !ok {
			t.Errorf("status response missing field: %s", field)
		}
	}

	// Test backward compatibility: EMA fields should be present
	emaFields := []string{"avg_download_ms", "avg_upload_ms", "avg_total_ms"}
	for _, field := range emaFields {
		if _, ok := status[field]; !ok {
			t.Errorf("status response missing backward-compatible EMA field: %s", field)
		}
	}
}

func TestOAuthTokenStore(t *testing.T) {
	db := testutil.SetupTestDB(t)
	store := &oauthTokenStore{db: db}

	ctx := context.Background()
	provider := "test-provider"
	expiry := time.Now().Add(1 * time.Hour)

	// Test UpsertOAuthToken
	err := store.UpsertOAuthToken(ctx, provider, "access", "refresh", expiry, "raw-json")
	if err != nil {
		t.Fatalf("UpsertOAuthToken() error = %v", err)
	}

	// Test GetOAuthToken
	access, refresh, exp, raw, err := store.GetOAuthToken(ctx, provider)
	if err != nil {
		t.Fatalf("GetOAuthToken() error = %v", err)
	}

	if access != "access" {
		t.Errorf("access = %s, want access", access)
	}
	if refresh != "refresh" {
		t.Errorf("refresh = %s, want refresh", refresh)
	}
	// raw is not stored/returned by this implementation
	_ = raw
	// Verify expiry is close to what we set
	if exp.Sub(expiry).Abs() > time.Second {
		t.Errorf("expiry mismatch: got %v, want %v", exp, expiry)
	}
}

func TestOAuthTokenStore_NotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)
	store := &oauthTokenStore{db: db}

	ctx := context.Background()
	access, refresh, expiry, raw, err := store.GetOAuthToken(ctx, "nonexistent")

	// Should not error, return empty values
	if err != nil {
		t.Errorf("GetOAuthToken() for nonexistent should not error, got %v", err)
	}
	if access != "" || refresh != "" || raw != "" || !expiry.IsZero() {
		t.Error("GetOAuthToken() for nonexistent should return zero values")
	}
}

func TestParseFloat64Query(t *testing.T) {
	tests := []struct {
		name  string
		query string
		key   string
		def   float64
		want  float64
	}{
		{
			name:  "valid float",
			query: "?value=3.14",
			key:   "value",
			def:   0.0,
			want:  3.14,
		},
		{
			name:  "missing key uses default",
			query: "?other=123",
			key:   "value",
			def:   2.5,
			want:  2.5,
		},
		{
			name:  "invalid float uses default",
			query: "?value=abc",
			key:   "value",
			def:   1.0,
			want:  1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.query, nil)
			got := parseFloat64Query(req, tt.key, tt.def)
			if got != tt.want {
				t.Errorf("parseFloat64Query() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseIntQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		key   string
		def   int
		want  int
	}{
		{
			name:  "valid int",
			query: "?value=42",
			key:   "value",
			def:   0,
			want:  42,
		},
		{
			name:  "missing key uses default",
			query: "?other=123",
			key:   "value",
			def:   10,
			want:  10,
		},
		{
			name:  "invalid int uses default",
			query: "?value=abc",
			key:   "value",
			def:   5,
			want:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.query, nil)
			got := parseIntQuery(req, tt.key, tt.def)
			if got != tt.want {
				t.Errorf("parseIntQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDerivePercent(t *testing.T) {
	tests := []struct {
		state string
		want  *float64
	}{
		{"", nil},
		{"unknown", nil},
		{"[download] 50.0% of 100MiB", ptrFloat64(50.0)},
		{"[download] 75.5% of ~200MiB at 10MiB/s ETA 00:10", ptrFloat64(75.5)},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			got := derivePercent(tt.state)
			if tt.want == nil {
				if got != nil {
					t.Errorf("derivePercent(%q) = %v, want nil", tt.state, *got)
				}
			} else {
				if got == nil {
					t.Errorf("derivePercent(%q) = nil, want %v", tt.state, *tt.want)
				} else if *got != *tt.want {
					t.Errorf("derivePercent(%q) = %v, want %v", tt.state, *got, *tt.want)
				}
			}
		})
	}
}

func ptrFloat64(f float64) *float64 {
	return &f
}
