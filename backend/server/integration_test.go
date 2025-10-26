package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/onnwee/vod-tender/backend/testutil"
)

// TestAdminEndpointsProtection validates that admin endpoints are protected when auth is configured
func TestAdminEndpointsProtection(t *testing.T) {
	db := testutil.SetupTestDB(t)
	
	tests := []struct {
		name           string
		path           string
		authHeader     string
		basicAuth      bool
		username       string
		password       string
		expectedStatus int
	}{
		{
			name:           "admin endpoint without auth - fails when configured",
			path:           "/admin/monitor",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "admin endpoint with valid basic auth",
			path:           "/admin/monitor",
			basicAuth:      true,
			username:       "admin",
			password:       "secret123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "admin endpoint with invalid basic auth",
			path:           "/admin/monitor",
			basicAuth:      true,
			username:       "admin",
			password:       "wrong",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "admin endpoint with valid token",
			path:           "/admin/monitor",
			authHeader:     "test-token-12345",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "admin endpoint with invalid token",
			path:           "/admin/monitor",
			authHeader:     "wrong-token",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up auth config
			os.Setenv("ADMIN_USERNAME", "admin")
			os.Setenv("ADMIN_PASSWORD", "secret123")
			os.Setenv("ADMIN_TOKEN", "test-token-12345")
			defer func() {
				os.Unsetenv("ADMIN_USERNAME")
				os.Unsetenv("ADMIN_PASSWORD")
				os.Unsetenv("ADMIN_TOKEN")
			}()

			handler := NewMux(db)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.basicAuth {
				req.SetBasicAuth(tt.username, tt.password)
			}
			if tt.authHeader != "" {
				req.Header.Set("X-Admin-Token", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

// TestRateLimitingOnAdminEndpoints validates that admin endpoints are rate limited
func TestRateLimitingOnAdminEndpoints(t *testing.T) {
	db := testutil.SetupTestDB(t)
	
	// Configure low rate limit for testing
	os.Setenv("RATE_LIMIT_ENABLED", "1")
	os.Setenv("RATE_LIMIT_REQUESTS_PER_IP", "3")
	os.Setenv("RATE_LIMIT_WINDOW_SECONDS", "60")
	os.Setenv("ADMIN_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("RATE_LIMIT_ENABLED")
		os.Unsetenv("RATE_LIMIT_REQUESTS_PER_IP")
		os.Unsetenv("RATE_LIMIT_WINDOW_SECONDS")
		os.Unsetenv("ADMIN_TOKEN")
	}()

	handler := NewMux(db)

	// Make 3 requests (should all succeed)
	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/admin/monitor", nil)
		req.Header.Set("X-Admin-Token", "test-token")
		req.RemoteAddr = "192.168.1.100:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/admin/monitor", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	req.RemoteAddr = "192.168.1.100:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 (rate limited), got %d", rr.Code)
	}

	// Check Retry-After header
	if retryAfter := rr.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("expected Retry-After header on rate limited response")
	}
}

// TestCORSRestricted validates CORS restrictions in production mode
func TestCORSRestricted(t *testing.T) {
	db := testutil.SetupTestDB(t)

	tests := []struct {
		name          string
		env           string
		allowedOrigins string
		requestOrigin string
		expectAllowed bool
	}{
		{
			name:          "dev mode allows any origin",
			env:           "dev",
			requestOrigin: "https://evil.com",
			expectAllowed: true,
		},
		{
			name:           "production mode blocks unlisted origin",
			env:            "production",
			allowedOrigins: "https://app.example.com",
			requestOrigin:  "https://evil.com",
			expectAllowed:  false,
		},
		{
			name:           "production mode allows listed origin",
			env:            "production",
			allowedOrigins: "https://app.example.com,https://admin.example.com",
			requestOrigin:  "https://app.example.com",
			expectAllowed:  true,
		},
		{
			name:           "production mode with wildcard subdomain",
			env:            "production",
			allowedOrigins: "*.example.com",
			requestOrigin:  "https://api.example.com",
			expectAllowed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("ENV", tt.env)
			if tt.allowedOrigins != "" {
				os.Setenv("CORS_ALLOWED_ORIGINS", tt.allowedOrigins)
			}
			defer func() {
				os.Unsetenv("ENV")
				os.Unsetenv("CORS_ALLOWED_ORIGINS")
			}()

			handler := NewMux(db)

			req := httptest.NewRequest(http.MethodGet, "/status", nil)
			req.Header.Set("Origin", tt.requestOrigin)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			allowOrigin := rr.Header().Get("Access-Control-Allow-Origin")
			if tt.expectAllowed {
				if allowOrigin == "" {
					t.Error("expected CORS to allow origin, but Access-Control-Allow-Origin header is empty")
				}
			} else {
				if allowOrigin == tt.requestOrigin {
					t.Errorf("expected CORS to block origin %s, but it was allowed", tt.requestOrigin)
				}
			}
		})
	}
}

// TestPublicEndpointsUnprotected validates that public endpoints remain accessible
func TestPublicEndpointsUnprotected(t *testing.T) {
	db := testutil.SetupTestDB(t)
	
	// Configure strict auth
	os.Setenv("ADMIN_USERNAME", "admin")
	os.Setenv("ADMIN_PASSWORD", "secret")
	defer func() {
		os.Unsetenv("ADMIN_USERNAME")
		os.Unsetenv("ADMIN_PASSWORD")
	}()

	handler := NewMux(db)

	// These endpoints should work without auth
	publicEndpoints := []string{
		"/healthz",
		"/readyz",
		"/metrics",
		"/status",
		"/vods",
		"/config",
	}

	for _, path := range publicEndpoints {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Should not be unauthorized (401)
			if rr.Code == http.StatusUnauthorized {
				t.Errorf("public endpoint %s should not require auth, got 401", path)
			}
		})
	}
}
