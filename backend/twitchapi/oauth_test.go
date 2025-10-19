package twitchapi

import (
	"strings"
	"testing"
	"time"
)

func TestBuildAuthorizeURL(t *testing.T) {
	tests := []struct {
		name        string
		clientID    string
		redirectURI string
		scopes      string
		state       string
		wantErr     bool
		wantParts   []string
	}{
		{
			name:        "valid request",
			clientID:    "test-client-id",
			redirectURI: "http://localhost/callback",
			scopes:      "user:read:email chat:read",
			state:       "random-state",
			wantErr:     false,
			wantParts:   []string{"client_id=test-client-id", "state=random-state", "scope="},
		},
		{
			name:        "empty client ID",
			clientID:    "",
			redirectURI: "http://localhost/callback",
			scopes:      "user:read:email",
			state:       "state",
			wantErr:     true,
		},
		{
			name:        "empty redirect URI",
			clientID:    "client",
			redirectURI: "",
			scopes:      "user:read:email",
			state:       "state",
			wantErr:     true,
		},
		{
			name:        "with scopes",
			clientID:    "client-id",
			redirectURI: "http://localhost/callback",
			scopes:      "user:read:email,chat:read",
			state:       "state-123",
			wantErr:     false,
			wantParts:   []string{"client_id=client-id", "scope=user%3Aread%3Aemail+chat%3Aread"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := BuildAuthorizeURL(tt.clientID, tt.redirectURI, tt.scopes, tt.state)
			
			if tt.wantErr {
				if err == nil {
					t.Error("BuildAuthorizeURL() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("BuildAuthorizeURL() unexpected error = %v", err)
				return
			}

			// Check URL contains expected parts
			for _, part := range tt.wantParts {
				if !strings.Contains(url, part) {
					t.Errorf("URL missing expected part %q: %s", part, url)
				}
			}

			// Should start with Twitch auth endpoint
			if !strings.HasPrefix(url, "https://id.twitch.tv/oauth2/authorize") {
				t.Errorf("URL doesn't start with Twitch auth endpoint: %s", url)
			}
		})
	}
}

func TestComputeExpiry(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn int
		wantAfter time.Duration
	}{
		{
			name:      "4 hours",
			expiresIn: 14400,
			wantAfter: 4 * time.Hour,
		},
		{
			name:      "1 hour",
			expiresIn: 3600,
			wantAfter: 1 * time.Hour,
		},
		{
			name:      "zero defaults to 60 minutes",
			expiresIn: 0,
			wantAfter: 60 * time.Minute,
		},
		{
			name:      "negative defaults to 60 minutes",
			expiresIn: -100,
			wantAfter: 60 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now()
			expiry := ComputeExpiry(tt.expiresIn)
			after := time.Now()

			expectedExpiry := before.Add(tt.wantAfter)
			
			// Allow 2 second tolerance
			if expiry.Before(expectedExpiry.Add(-2*time.Second)) || expiry.After(after.Add(tt.wantAfter).Add(2*time.Second)) {
				t.Errorf("ComputeExpiry(%d) = %v, want approximately %v", tt.expiresIn, expiry, expectedExpiry)
			}
		})
	}
}

func TestAuthCodeExchangeResult(t *testing.T) {
	// Test that struct can be created and has expected fields
	result := AuthCodeExchangeResult{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		ExpiresIn:    3600,
		Scope:        []string{"user:read:email"},
		TokenType:    "bearer",
	}

	if result.AccessToken != "access-123" {
		t.Errorf("AccessToken = %s, want access-123", result.AccessToken)
	}
	if result.RefreshToken != "refresh-456" {
		t.Errorf("RefreshToken = %s, want refresh-456", result.RefreshToken)
	}
	if result.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d, want 3600", result.ExpiresIn)
	}
	if len(result.Scope) != 1 || result.Scope[0] != "user:read:email" {
		t.Errorf("Scope = %v, want [user:read:email]", result.Scope)
	}
}

func TestRefreshResult(t *testing.T) {
	// Test that struct can be created and has expected fields
	result := RefreshResult{
		AccessToken:  "new-access-123",
		RefreshToken: "new-refresh-456",
		ExpiresIn:    7200,
		Scope:        []string{"chat:read", "chat:edit"},
		TokenType:    "bearer",
	}

	if result.AccessToken != "new-access-123" {
		t.Errorf("AccessToken = %s, want new-access-123", result.AccessToken)
	}
	if result.RefreshToken != "new-refresh-456" {
		t.Errorf("RefreshToken = %s, want new-refresh-456", result.RefreshToken)
	}
	if result.ExpiresIn != 7200 {
		t.Errorf("ExpiresIn = %d, want 7200", result.ExpiresIn)
	}
	if len(result.Scope) != 2 {
		t.Errorf("Scope length = %d, want 2", len(result.Scope))
	}
}
