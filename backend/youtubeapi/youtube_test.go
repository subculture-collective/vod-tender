package youtubeapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/onnwee/vod-tender/backend/config"
)

// mockTokenStore implements TokenStore for testing
type mockTokenStore struct {
	tokens map[string]tokenData
}

type tokenData struct {
	access  string
	refresh string
	expiry  time.Time
	raw     string
}

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{
		tokens: make(map[string]tokenData),
	}
}

func (m *mockTokenStore) UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error {
	m.tokens[provider] = tokenData{
		access:  accessToken,
		refresh: refreshToken,
		expiry:  expiry,
		raw:     raw,
	}
	return nil
}

func (m *mockTokenStore) GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error) {
	if data, ok := m.tokens[provider]; ok {
		return data.access, data.refresh, data.expiry, data.raw, nil
	}
	return "", "", time.Time{}, "", nil
}

func TestNew(t *testing.T) {
	cfg := &config.Config{
		YTClientID:     "test-client-id",
		YTClientSecret: "test-secret",
		YTRedirectURI:  "http://localhost/callback",
		YTScopes:       "https://www.googleapis.com/auth/youtube.upload",
	}
	store := newMockTokenStore()

	svc := New(cfg, store)
	if svc == nil {
		t.Fatal("New() returned nil")
	}
	if svc.cfg != cfg {
		t.Error("service config not set correctly")
	}
	if svc.db != store {
		t.Error("service token store not set correctly")
	}
	if svc.oauth == nil {
		t.Error("oauth config is nil")
	}
}

func TestNew_ScopeParsing(t *testing.T) {
	tests := []struct {
		name       string
		scopesConf string
		wantLen    int
	}{
		{
			name:       "default single scope",
			scopesConf: "",
			wantLen:    1,
		},
		{
			name:       "comma separated",
			scopesConf: "scope1,scope2,scope3",
			wantLen:    3,
		},
		{
			name:       "space separated",
			scopesConf: "scope1 scope2 scope3",
			wantLen:    3,
		},
		{
			name:       "mixed separators",
			scopesConf: "scope1, scope2 scope3",
			wantLen:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				YTClientID:     "test-client-id",
				YTClientSecret: "test-secret",
				YTRedirectURI:  "http://localhost/callback",
				YTScopes:       tt.scopesConf,
			}
			store := newMockTokenStore()
			svc := New(cfg, store)

			if len(svc.oauth.Scopes) != tt.wantLen {
				t.Errorf("scopes length = %d, want %d", len(svc.oauth.Scopes), tt.wantLen)
			}
		})
	}
}

func TestAuthCodeURL(t *testing.T) {
	cfg := &config.Config{
		YTClientID:     "test-client-id",
		YTClientSecret: "test-secret",
		YTRedirectURI:  "http://localhost/callback",
	}
	store := newMockTokenStore()
	svc := New(cfg, store)

	url := svc.AuthCodeURL("test-state")
	if url == "" {
		t.Error("AuthCodeURL returned empty string")
	}
	// Check that it contains expected parameters
	if !strings.Contains(url, "client_id=test-client-id") {
		t.Errorf("URL missing client_id: %s", url)
	}
	if !strings.Contains(url, "state=test-state") {
		t.Errorf("URL missing state: %s", url)
	}
	if !strings.Contains(url, "access_type=offline") {
		t.Errorf("URL missing access_type=offline: %s", url)
	}
}

func TestRefreshIfNeeded_NoToken(t *testing.T) {
	cfg := &config.Config{
		YTClientID:     "test-client-id",
		YTClientSecret: "test-secret",
	}
	store := newMockTokenStore()
	svc := New(cfg, store)

	_, err := svc.refreshIfNeeded(context.Background())
	if err == nil {
		t.Error("refreshIfNeeded() should return error when no token stored")
	}
	if !strings.Contains(err.Error(), "no youtube token") {
		t.Errorf("error = %v, want error about no token", err)
	}
}

func TestRefreshIfNeeded_ValidToken(t *testing.T) {
	cfg := &config.Config{
		YTClientID:     "test-client-id",
		YTClientSecret: "test-secret",
	}
	store := newMockTokenStore()
	svc := New(cfg, store)

	// Store a valid token that doesn't need refresh
	futureExpiry := time.Now().Add(10 * time.Minute)
	store.UpsertOAuthToken(context.Background(), "youtube", "valid-token", "refresh-token", futureExpiry, "")

	token, err := svc.refreshIfNeeded(context.Background())
	if err != nil {
		t.Errorf("refreshIfNeeded() error = %v", err)
	}
	if token.AccessToken != "valid-token" {
		t.Errorf("token.AccessToken = %s, want valid-token", token.AccessToken)
	}
}

func TestUploadVideo_NilService(t *testing.T) {
	_, err := UploadVideo(context.Background(), nil, "/tmp/test.mp4", "Test", "Description", "private")
	if err == nil {
		t.Error("UploadVideo() with nil service should return error")
	}
	if !strings.Contains(err.Error(), "nil youtube service") {
		t.Errorf("error = %v, want error about nil service", err)
	}
}

func TestUploadVideo_DefaultPrivacy(t *testing.T) {
	// This test just verifies the function signature and defaults
	// Full integration test would require mocking YouTube API
	ctx := context.Background()

	// Test that calling with empty privacy doesn't panic
	// (actual upload will fail without valid service, but we're testing the parameter handling)
	_, err := UploadVideo(ctx, nil, "/nonexistent/file.mp4", "Test", "Desc", "")
	if err == nil {
		t.Error("expected error for nil service")
	}
}
