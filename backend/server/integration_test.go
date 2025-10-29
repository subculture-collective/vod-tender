package server

import (
	"net/http"
	"net/http/httptest"
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
			t.Setenv("ADMIN_USERNAME", "admin")
			t.Setenv("ADMIN_PASSWORD", "secret123")
			t.Setenv("ADMIN_TOKEN", "test-token-12345")

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
	t.Setenv("RATE_LIMIT_ENABLED", "1")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_IP", "3")
	t.Setenv("RATE_LIMIT_WINDOW_SECONDS", "60")
	t.Setenv("ADMIN_TOKEN", "test-token")

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
		name           string
		env            string
		allowedOrigins string
		requestOrigin  string
		expectAllowed  bool
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
			t.Setenv("ENV", tt.env)
			if tt.allowedOrigins != "" {
				t.Setenv("CORS_ALLOWED_ORIGINS", tt.allowedOrigins)
			}

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
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")

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

// TestRateLimitingPathMatching verifies that rate limiting is only applied to specific VOD endpoints
func TestRateLimitingPathMatching(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Enable rate limiting with very low limit for testing
	t.Setenv("RATE_LIMIT_ENABLED", "1")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_IP", "1")
	t.Setenv("RATE_LIMIT_WINDOW_SECONDS", "60")

	handler := NewMux(db)

	tests := []struct {
		name                 string
		path                 string
		shouldBeRateLimited  bool
		description          string
	}{
		// Paths that SHOULD be rate limited
		{
			name:                "vod cancel endpoint",
			path:                "/vods/123/cancel",
			shouldBeRateLimited: true,
			description:         "VOD cancel endpoint should be rate limited",
		},
		{
			name:                "vod reprocess endpoint",
			path:                "/vods/abc456/reprocess",
			shouldBeRateLimited: true,
			description:         "VOD reprocess endpoint should be rate limited",
		},
		
		// Paths that should NOT be rate limited (the bug this fixes)
		{
			name:                "generic cancel path",
			path:                "/anything/cancel",
			shouldBeRateLimited: false,
			description:         "Generic /anything/cancel should NOT be rate limited",
		},
		{
			name:                "generic reprocess path",
			path:                "/custom/reprocess",
			shouldBeRateLimited: false,
			description:         "Generic /custom/reprocess should NOT be rate limited",
		},
		{
			name:                "api cancel path",
			path:                "/api/cancel",
			shouldBeRateLimited: false,
			description:         "/api/cancel should NOT be rate limited",
		},
		{
			name:                "root cancel path",
			path:                "/cancel",
			shouldBeRateLimited: false,
			description:         "Root /cancel should NOT be rate limited",
		},
		{
			name:                "vod progress endpoint",
			path:                "/vods/123/progress",
			shouldBeRateLimited: false,
			description:         "Other VOD endpoints should NOT be rate limited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make first request
			req1 := httptest.NewRequest(http.MethodPost, tt.path, nil)
			req1.RemoteAddr = "192.168.1.100:12345"
			rr1 := httptest.NewRecorder()
			handler.ServeHTTP(rr1, req1)

			// Make second request immediately (should hit rate limit if path is rate limited)
			req2 := httptest.NewRequest(http.MethodPost, tt.path, nil)
			req2.RemoteAddr = "192.168.1.100:12345"
			rr2 := httptest.NewRecorder()
			handler.ServeHTTP(rr2, req2)

			if tt.shouldBeRateLimited {
				// Second request should be rate limited (429)
				if rr2.Code != http.StatusTooManyRequests {
					t.Errorf("%s: expected rate limit (429) on second request, got %d", tt.description, rr2.Code)
				}
			} else {
				// Second request should NOT be rate limited
				if rr2.Code == http.StatusTooManyRequests {
					t.Errorf("%s: should NOT be rate limited, but got 429", tt.description)
				}
			}
		})
	}
}
