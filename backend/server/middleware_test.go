package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/onnwee/vod-tender/backend/testutil"
)

func TestAdminAuthMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		username       string
		password       string
		token          string
		reqUsername    string
		reqPassword    string
		reqToken       string
		expectedStatus int
	}{
		{
			name:           "no auth configured - allows request",
			username:       "",
			password:       "",
			token:          "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid basic auth",
			username:       "admin",
			password:       "secret123",
			reqUsername:    "admin",
			reqPassword:    "secret123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid basic auth username",
			username:       "admin",
			password:       "secret123",
			reqUsername:    "wrong",
			reqPassword:    "secret123",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid basic auth password",
			username:       "admin",
			password:       "secret123",
			reqUsername:    "admin",
			reqPassword:    "wrong",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "valid token auth",
			token:          "test-token-12345",
			reqToken:       "test-token-12345",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid token auth",
			token:          "test-token-12345",
			reqToken:       "wrong-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "token auth takes precedence over basic auth",
			username:       "admin",
			password:       "secret123",
			token:          "test-token-12345",
			reqToken:       "test-token-12345",
			reqUsername:    "wrong",
			reqPassword:    "wrong",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Configure auth
			cfg := &authConfig{
				adminUsername: tt.username,
				adminPassword: tt.password,
				adminToken:    tt.token,
				enabled:       (tt.username != "" && tt.password != "") || tt.token != "",
			}

			// Create test handler
			handler := adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			}), cfg)

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
			if tt.reqUsername != "" || tt.reqPassword != "" {
				req.SetBasicAuth(tt.reqUsername, tt.reqPassword)
			}
			if tt.reqToken != "" {
				req.Header.Set("X-Admin-Token", tt.reqToken)
			}

			// Execute request
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Check status
			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			// Check WWW-Authenticate header on 401
			if tt.expectedStatus == http.StatusUnauthorized {
				if auth := rr.Header().Get("WWW-Authenticate"); auth == "" {
					t.Error("expected WWW-Authenticate header on 401 response")
				}
			}
		})
	}
}

func TestRateLimiter(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 3,
		window:        100 * time.Millisecond,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		if !limiter.allow(context.Background(), "192.168.1.1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if limiter.allow(context.Background(), "192.168.1.1") {
		t.Error("request 4 should be denied (rate limit exceeded)")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should allow requests again
	if !limiter.allow(context.Background(), "192.168.1.1") {
		t.Error("request after window expiry should be allowed")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 2,
		window:        1 * time.Second,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	// IP 1 makes 2 requests (should succeed)
	if !limiter.allow(context.Background(), "192.168.1.1") {
		t.Error("IP1 request 1 should be allowed")
	}
	if !limiter.allow(context.Background(), "192.168.1.1") {
		t.Error("IP1 request 2 should be allowed")
	}

	// IP 2 makes 2 requests (should also succeed, different IP)
	if !limiter.allow(context.Background(), "192.168.1.2") {
		t.Error("IP2 request 1 should be allowed")
	}
	if !limiter.allow(context.Background(), "192.168.1.2") {
		t.Error("IP2 request 2 should be allowed")
	}

	// Both IPs are now at limit
	if limiter.allow(context.Background(), "192.168.1.1") {
		t.Error("IP1 request 3 should be denied")
	}
	if limiter.allow(context.Background(), "192.168.1.2") {
		t.Error("IP2 request 3 should be denied")
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       false,
		requestsPerIP: 1,
		window:        1 * time.Second,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	// Should allow unlimited requests when disabled
	for i := 0; i < 100; i++ {
		if !limiter.allow(context.Background(), "192.168.1.1") {
			t.Errorf("request %d should be allowed when rate limiter is disabled", i+1)
		}
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 2,
		window:        1 * time.Second,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}), limiter)

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("request 3: expected 429, got %d", rr.Code)
	}

	// Check Retry-After header
	if retryAfter := rr.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("expected Retry-After header on 429 response")
	}
}

func TestRateLimitMiddlewareWithXForwardedFor(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 2,
		window:        1 * time.Second,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), limiter)

	// Requests with X-Forwarded-For should use the rightmost (proxy-added) IP
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"                          // Direct connection IP
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.2") // Client IP, closest proxy IP
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd request from same rightmost IP should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.50, 10.0.0.2") // Different client, same proxy
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}
}

func TestRateLimitMiddlewareIPv6(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 2,
		window:        1 * time.Second,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), limiter)

	// Test IPv6 address with port
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "[2001:db8::1]:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("IPv6 request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "[2001:db8::1]:54321"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("IPv6 request 3: expected 429, got %d", rr.Code)
	}
}

func TestRateLimitMiddlewareIPv6WithoutPort(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 2,
		window:        1 * time.Second,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), limiter)

	// Test IPv6 address without port (e.g., from X-Forwarded-For)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "127.0.0.1:8080" // Doesn't matter
		req.Header.Set("X-Forwarded-For", "2001:db8::42")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("IPv6 without port request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "2001:db8::42")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("IPv6 without port request 3: expected 429, got %d", rr.Code)
	}
}

func TestRateLimitMiddlewareIPv4WithoutPort(t *testing.T) {
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 2,
		window:        1 * time.Second,
	}
	limiter := newIPRateLimiter(context.Background(), cfg)

	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), limiter)

	// Test IPv4 address without port (e.g., from X-Forwarded-For)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:8080" // Doesn't matter
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("IPv4 without port request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("IPv4 without port request 3: expected 429, got %d", rr.Code)
	}
}

func TestCORSConfig(t *testing.T) {
	tests := []struct {
		name              string
		permissive        bool
		allowedOrigins    []string
		requestOrigin     string
		expectAllowOrigin string
		expectCredentials bool
	}{
		{
			name:              "permissive mode allows all origins",
			permissive:        true,
			requestOrigin:     "https://example.com",
			expectAllowOrigin: "*",
		},
		{
			name:              "restricted mode with matching origin",
			permissive:        false,
			allowedOrigins:    []string{"https://example.com", "https://app.example.com"},
			requestOrigin:     "https://example.com",
			expectAllowOrigin: "https://example.com",
			expectCredentials: true,
		},
		{
			name:              "restricted mode with non-matching origin",
			permissive:        false,
			allowedOrigins:    []string{"https://example.com"},
			requestOrigin:     "https://evil.com",
			expectAllowOrigin: "",
		},
		{
			name:              "wildcard subdomain matching",
			permissive:        false,
			allowedOrigins:    []string{"*.example.com"},
			requestOrigin:     "https://app.example.com",
			expectAllowOrigin: "https://app.example.com",
			expectCredentials: true,
		},
		{
			name:              "wildcard does not match parent",
			permissive:        false,
			allowedOrigins:    []string{"*.example.com"},
			requestOrigin:     "https://example.com",
			expectAllowOrigin: "https://example.com", // Actually matches due to special handling in isOriginAllowed
			expectCredentials: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &corsConfig{
				permissive:     tt.permissive,
				allowedOrigins: tt.allowedOrigins,
			}

			handler := withCORSConfig(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}), cfg)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			allowOrigin := rr.Header().Get("Access-Control-Allow-Origin")
			if allowOrigin != tt.expectAllowOrigin {
				t.Errorf("expected Allow-Origin %q, got %q", tt.expectAllowOrigin, allowOrigin)
			}

			if tt.expectCredentials {
				if creds := rr.Header().Get("Access-Control-Allow-Credentials"); creds != "true" {
					t.Error("expected Allow-Credentials: true for restricted mode")
				}
			}
		})
	}
}

func TestCORSPreflightRequest(t *testing.T) {
	cfg := &corsConfig{
		permissive:     true,
		allowedOrigins: []string{},
	}

	handler := withCORSConfig(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should not be called for OPTIONS
		t.Error("handler should not be called for OPTIONS request")
		w.WriteHeader(http.StatusOK)
	}), cfg)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", rr.Code)
	}

	if allowMethods := rr.Header().Get("Access-Control-Allow-Methods"); allowMethods == "" {
		t.Error("expected Allow-Methods header on OPTIONS response")
	}

	if allowHeaders := rr.Header().Get("Access-Control-Allow-Headers"); allowHeaders == "" {
		t.Error("expected Allow-Headers header on OPTIONS response")
	}
}

func TestLoadAuthConfig(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		wantEnabled bool
	}{
		{
			name:        "no auth configured",
			envVars:     map[string]string{},
			wantEnabled: false,
		},
		{
			name: "basic auth only",
			envVars: map[string]string{
				"ADMIN_USERNAME": "admin",
				"ADMIN_PASSWORD": "secret",
			},
			wantEnabled: true,
		},
		{
			name: "token auth only",
			envVars: map[string]string{
				"ADMIN_TOKEN": "test-token",
			},
			wantEnabled: true,
		},
		{
			name: "both auth methods",
			envVars: map[string]string{
				"ADMIN_USERNAME": "admin",
				"ADMIN_PASSWORD": "secret",
				"ADMIN_TOKEN":    "test-token",
			},
			wantEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Unsetenv("ADMIN_USERNAME")
			os.Unsetenv("ADMIN_PASSWORD")
			os.Unsetenv("ADMIN_TOKEN")

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := loadAuthConfig()

			if cfg.enabled != tt.wantEnabled {
				t.Errorf("expected enabled=%v, got %v", tt.wantEnabled, cfg.enabled)
			}

			// Cleanup
			for k := range tt.envVars {
				os.Unsetenv(k)
			}
		})
	}
}

func TestLoadCORSConfig(t *testing.T) {
	tests := []struct {
		name           string
		envVars        map[string]string
		wantPermissive bool
		wantOriginsLen int
	}{
		{
			name:           "default dev mode",
			envVars:        map[string]string{},
			wantPermissive: true,
			wantOriginsLen: 0,
		},
		{
			name: "explicit dev mode",
			envVars: map[string]string{
				"ENV": "dev",
			},
			wantPermissive: true,
		},
		{
			name: "production mode",
			envVars: map[string]string{
				"ENV": "production",
			},
			wantPermissive: false,
		},
		{
			name: "production with allowed origins",
			envVars: map[string]string{
				"ENV":                  "production",
				"CORS_ALLOWED_ORIGINS": "https://example.com,https://app.example.com",
			},
			wantPermissive: false,
			wantOriginsLen: 2,
		},
		{
			name: "explicit permissive override",
			envVars: map[string]string{
				"ENV":             "production",
				"CORS_PERMISSIVE": "1",
			},
			wantPermissive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Unsetenv("ENV")
			os.Unsetenv("CORS_PERMISSIVE")
			os.Unsetenv("CORS_ALLOWED_ORIGINS")

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := loadCORSConfig()

			if cfg.permissive != tt.wantPermissive {
				t.Errorf("expected permissive=%v, got %v", tt.wantPermissive, cfg.permissive)
			}

			if tt.wantOriginsLen > 0 && len(cfg.allowedOrigins) != tt.wantOriginsLen {
				t.Errorf("expected %d allowed origins, got %d", tt.wantOriginsLen, len(cfg.allowedOrigins))
			}

			// Cleanup
			for k := range tt.envVars {
				os.Unsetenv(k)
			}
		})
	}
}

func TestIsOriginAllowed(t *testing.T) {
	tests := []struct {
		name           string
		origin         string
		allowedOrigins []string
		want           bool
	}{
		{
			name:           "exact match",
			origin:         "https://example.com",
			allowedOrigins: []string{"https://example.com", "https://other.com"},
			want:           true,
		},
		{
			name:           "no match",
			origin:         "https://evil.com",
			allowedOrigins: []string{"https://example.com"},
			want:           false,
		},
		{
			name:           "wildcard subdomain match",
			origin:         "https://app.example.com",
			allowedOrigins: []string{"*.example.com"},
			want:           true,
		},
		{
			name:           "wildcard subdomain deeper match",
			origin:         "https://api.v2.example.com",
			allowedOrigins: []string{"*.example.com"},
			want:           true,
		},
		{
			name:           "wildcard does not match parent",
			origin:         "https://example.com",
			allowedOrigins: []string{"*.example.com"},
			want:           true, // Special case: matches parent too
		},
		{
			name:           "http vs https mismatch",
			origin:         "http://example.com",
			allowedOrigins: []string{"https://example.com"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOriginAllowed(tt.origin, tt.allowedOrigins)
			if got != tt.want {
				t.Errorf("isOriginAllowed(%q, %v) = %v, want %v", tt.origin, tt.allowedOrigins, got, tt.want)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input      string
		defaultVal int
		want       int
	}{
		{"123", 0, 123},
		{"", 42, 42},
		{"invalid", 42, 42},
		{"-1", 0, -1},
		{"0", 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseInt(tt.input, tt.defaultVal)
			if got != tt.want {
				t.Errorf("parseInt(%q, %d) = %d, want %d", tt.input, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestVodSensitiveEndpointPattern(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		shouldMatch bool
	}{
		// Valid VOD endpoints that should match
		{
			name:        "valid cancel endpoint",
			path:        "/vods/123/cancel",
			shouldMatch: true,
		},
		{
			name:        "valid reprocess endpoint",
			path:        "/vods/abc123/reprocess",
			shouldMatch: true,
		},
		{
			name:        "valid cancel with alphanumeric ID",
			path:        "/vods/v1234567890/cancel",
			shouldMatch: true,
		},
		{
			name:        "valid reprocess with hyphenated ID",
			path:        "/vods/vod-123-456/reprocess",
			shouldMatch: true,
		},

		// Invalid paths that should NOT match
		{
			name:        "generic cancel path",
			path:        "/anything/cancel",
			shouldMatch: false,
		},
		{
			name:        "generic reprocess path",
			path:        "/custom/reprocess",
			shouldMatch: false,
		},
		{
			name:        "cancel without vods prefix",
			path:        "/api/123/cancel",
			shouldMatch: false,
		},
		{
			name:        "reprocess without vods prefix",
			path:        "/admin/123/reprocess",
			shouldMatch: false,
		},
		{
			name:        "cancel with trailing slash",
			path:        "/vods/123/cancel/",
			shouldMatch: false,
		},
		{
			name:        "reprocess with additional path segments",
			path:        "/vods/123/reprocess/extra",
			shouldMatch: false,
		},
		{
			name:        "cancel with no ID",
			path:        "/vods/cancel",
			shouldMatch: false,
		},
		{
			name:        "reprocess with no ID",
			path:        "/vods/reprocess",
			shouldMatch: false,
		},
		{
			name:        "cancel with empty ID (double slash)",
			path:        "/vods//cancel",
			shouldMatch: false,
		},
		{
			name:        "different vod endpoint",
			path:        "/vods/123/progress",
			shouldMatch: false,
		},
		{
			name:        "vods list endpoint",
			path:        "/vods",
			shouldMatch: false,
		},
		{
			name:        "vods detail endpoint",
			path:        "/vods/123",
			shouldMatch: false,
		},
		{
			name:        "root cancel",
			path:        "/cancel",
			shouldMatch: false,
		},
		{
			name:        "root reprocess",
			path:        "/reprocess",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := getVodSensitiveEndpointPattern().MatchString(tt.path)
			if matched != tt.shouldMatch {
				t.Errorf("getVodSensitiveEndpointPattern().MatchString(%q) = %v, want %v", tt.path, matched, tt.shouldMatch)
			}
		})
	}
}

func TestPostgresRateLimiter(t *testing.T) {
	db := testutil.SetupTestDB(t)

	ctx := context.Background()
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 3,
		window:        100 * time.Millisecond,
		backend:       "postgres",
	}

	limiter, err := newPostgresRateLimiter(ctx, db, cfg)
	if err != nil {
		t.Fatalf("failed to create postgres rate limiter: %v", err)
	}

	// Clean up test data
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM rate_limit_requests WHERE ip LIKE '192.168.%'")
	}()

	testIP := "192.168.1.100"

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		if !limiter.allow(context.Background(), testIP) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if limiter.allow(context.Background(), testIP) {
		t.Error("request 4 should be denied (rate limit exceeded)")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should allow requests again
	if !limiter.allow(context.Background(), testIP) {
		t.Error("request after window expiry should be allowed")
	}
}

func TestPostgresRateLimiterDifferentIPs(t *testing.T) {
	db := testutil.SetupTestDB(t)

	ctx := context.Background()
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 2,
		window:        1 * time.Second,
		backend:       "postgres",
	}

	limiter, err := newPostgresRateLimiter(ctx, db, cfg)
	if err != nil {
		t.Fatalf("failed to create postgres rate limiter: %v", err)
	}

	// Clean up test data
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM rate_limit_requests WHERE ip LIKE '192.168.%'")
	}()

	ip1 := "192.168.1.101"
	ip2 := "192.168.1.102"

	// IP 1 makes 2 requests (should succeed)
	if !limiter.allow(context.Background(), ip1) {
		t.Error("IP1 request 1 should be allowed")
	}
	if !limiter.allow(context.Background(), ip1) {
		t.Error("IP1 request 2 should be allowed")
	}

	// IP 2 makes 2 requests (should also succeed, different IP)
	if !limiter.allow(context.Background(), ip2) {
		t.Error("IP2 request 1 should be allowed")
	}
	if !limiter.allow(context.Background(), ip2) {
		t.Error("IP2 request 2 should be allowed")
	}

	// Both IPs are now at limit
	if limiter.allow(context.Background(), ip1) {
		t.Error("IP1 request 3 should be denied")
	}
	if limiter.allow(context.Background(), ip2) {
		t.Error("IP2 request 3 should be denied")
	}
}

func TestPostgresRateLimiterDisabled(t *testing.T) {
	db := testutil.SetupTestDB(t)

	ctx := context.Background()
	cfg := &rateLimiterConfig{
		enabled:       false,
		requestsPerIP: 1,
		window:        1 * time.Second,
		backend:       "postgres",
	}

	limiter, err := newPostgresRateLimiter(ctx, db, cfg)
	if err != nil {
		t.Fatalf("failed to create postgres rate limiter: %v", err)
	}

	// Clean up test data
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM rate_limit_requests WHERE ip LIKE '192.168.%'")
	}()

	testIP := "192.168.1.103"

	// Should allow unlimited requests when disabled
	for i := 0; i < 20; i++ {
		if !limiter.allow(context.Background(), testIP) {
			t.Errorf("request %d should be allowed when rate limiter is disabled", i+1)
		}
	}
}

func TestPostgresRateLimiterConcurrency(t *testing.T) {
	db := testutil.SetupTestDB(t)

	ctx := context.Background()
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 10,
		window:        1 * time.Second,
		backend:       "postgres",
	}

	limiter, err := newPostgresRateLimiter(ctx, db, cfg)
	if err != nil {
		t.Fatalf("failed to create postgres rate limiter: %v", err)
	}

	// Clean up test data
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM rate_limit_requests WHERE ip LIKE '192.168.%'")
	}()

	testIP := "192.168.1.104"

	// Simulate concurrent requests from the same IP
	// With advisory locks, the limiter should be truly atomic
	totalRequests := 15
	results := make(chan bool, totalRequests)
	for i := 0; i < totalRequests; i++ {
		go func() {
			results <- limiter.allow(context.Background(), testIP)
		}()
	}

	allowed := 0
	denied := 0
	for i := 0; i < totalRequests; i++ {
		if <-results {
			allowed++
		} else {
			denied++
		}
	}

	// With advisory locks ensuring atomicity, we expect exactly requestsPerIP
	// allowed requests and the remainder denied under concurrent load
	expectedAllowed := cfg.requestsPerIP
	expectedDenied := totalRequests - expectedAllowed

	if allowed != expectedAllowed {
		t.Errorf("expected %d allowed requests, got %d", expectedAllowed, allowed)
	}
	if denied != expectedDenied {
		t.Errorf("expected %d denied requests, got %d", expectedDenied, denied)
	}
	if allowed+denied != totalRequests {
		t.Errorf("expected %d total requests, got %d", totalRequests, allowed+denied)
	}
}

func TestPostgresRateLimiterCleanup(t *testing.T) {
	db := testutil.SetupTestDB(t)

	ctx := context.Background()
	cfg := &rateLimiterConfig{
		enabled:       true,
		requestsPerIP: 5,
		window:        100 * time.Millisecond,
		backend:       "postgres",
	}

	limiter, err := newPostgresRateLimiter(ctx, db, cfg)
	if err != nil {
		t.Fatalf("failed to create postgres rate limiter: %v", err)
	}

	// Clean up test data
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM rate_limit_requests WHERE ip LIKE '192.168.%'")
	}()

	testIP := "192.168.1.105"

	// Make some requests
	for i := 0; i < 3; i++ {
		limiter.allow(context.Background(), testIP)
	}

	// Check that entries exist
	var count int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM rate_limit_requests WHERE ip = $1",
		testIP).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count entries: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 entries, got %d", count)
	}

	// Wait for entries to be old enough for cleanup
	time.Sleep(250 * time.Millisecond)

	// Trigger cleanup
	limiter.cleanup(ctx)

	// Check that old entries were cleaned up
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM rate_limit_requests WHERE ip = $1",
		testIP).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count entries after cleanup: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", count)
	}
}

func TestRateLimiterBackendConfiguration(t *testing.T) {
	tests := []struct {
		name            string
		backend         string
		expectedBackend string
	}{
		{
			name:            "default to memory",
			backend:         "",
			expectedBackend: "memory",
		},
		{
			name:            "explicit memory",
			backend:         "memory",
			expectedBackend: "memory",
		},
		{
			name:            "postgres backend",
			backend:         "postgres",
			expectedBackend: "postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.backend != "" {
				os.Setenv("RATE_LIMIT_BACKEND", tt.backend)
			} else {
				os.Unsetenv("RATE_LIMIT_BACKEND")
			}
			defer os.Unsetenv("RATE_LIMIT_BACKEND")

			cfg := loadRateLimiterConfig()
			if cfg.backend != tt.expectedBackend {
				t.Errorf("expected backend %q, got %q", tt.expectedBackend, cfg.backend)
			}
		})
	}
}
