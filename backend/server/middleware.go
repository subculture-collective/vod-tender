// Package server middleware for authentication and rate limiting
package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// authConfig holds authentication configuration loaded from environment
type authConfig struct {
	adminUsername string
	adminPassword string
	adminToken    string
	enabled       bool
}

// loadAuthConfig reads auth configuration from environment variables
func loadAuthConfig() *authConfig {
	username := os.Getenv("ADMIN_USERNAME")
	password := os.Getenv("ADMIN_PASSWORD")
	token := os.Getenv("ADMIN_TOKEN")
	
	// Auth is enabled if either basic auth (username+password) or token auth is configured
	enabled := (username != "" && password != "") || token != ""
	
	if !enabled {
		slog.Warn("Admin authentication not configured - admin endpoints are UNPROTECTED. Set ADMIN_USERNAME+ADMIN_PASSWORD or ADMIN_TOKEN for production")
	}
	
	return &authConfig{
		adminUsername: username,
		adminPassword: password,
		adminToken:    token,
		enabled:       enabled,
	}
}

// adminAuth is a middleware that protects admin endpoints with Basic Auth or token-based auth
func adminAuth(next http.Handler, cfg *authConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if not configured (dev mode)
		if !cfg.enabled {
			next.ServeHTTP(w, r)
			return
		}
		
		// Try token-based auth first (X-Admin-Token header)
		if cfg.adminToken != "" {
			token := r.Header.Get("X-Admin-Token")
			if token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(cfg.adminToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		
		// Try Basic Auth
		if cfg.adminUsername != "" && cfg.adminPassword != "" {
			username, password, ok := r.BasicAuth()
			if ok {
				usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(cfg.adminUsername)) == 1
				passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(cfg.adminPassword)) == 1
				if usernameMatch && passwordMatch {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		
		// Auth failed - return 401 with WWW-Authenticate header
		w.Header().Set("WWW-Authenticate", `Basic realm="vod-tender admin"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		slog.Warn("admin auth failed", slog.String("path", r.URL.Path), slog.String("remote_addr", r.RemoteAddr))
	})
}

// rateLimiterConfig holds rate limiting configuration
type rateLimiterConfig struct {
	enabled       bool
	requestsPerIP int           // Max requests per IP per window
	window        time.Duration // Time window for rate limiting
}

// loadRateLimiterConfig reads rate limiter configuration from environment
func loadRateLimiterConfig() *rateLimiterConfig {
	enabled := os.Getenv("RATE_LIMIT_ENABLED") != "0" // Enabled by default
	requestsPerIP := 10                                 // Default: 10 requests per window
	window := 1 * time.Minute                          // Default: 1 minute window
	
	// Allow environment override
	if v := os.Getenv("RATE_LIMIT_REQUESTS_PER_IP"); v != "" {
		// Parse integer
		if n := parseInt(v, requestsPerIP); n > 0 {
			requestsPerIP = n
		}
	}
	
	if v := os.Getenv("RATE_LIMIT_WINDOW_SECONDS"); v != "" {
		if n := parseInt(v, 60); n > 0 {
			window = time.Duration(n) * time.Second
		}
	}
	
	return &rateLimiterConfig{
		enabled:       enabled,
		requestsPerIP: requestsPerIP,
		window:        window,
	}
}

// ipRateLimiter implements a simple sliding window rate limiter per IP
type ipRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	cfg      *rateLimiterConfig
}

type visitor struct {
	requests  []time.Time
	lastClean time.Time
}

// newIPRateLimiter creates a new rate limiter
func newIPRateLimiter(ctx context.Context, cfg *rateLimiterConfig) *ipRateLimiter {
	limiter := &ipRateLimiter{
		visitors: make(map[string]*visitor),
		cfg:      cfg,
	}
	
	// Start cleanup goroutine to remove stale entries
	go limiter.cleanupLoop(ctx)
	
	return limiter
}

// cleanupLoop periodically removes stale visitor entries
func (rl *ipRateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-ctx.Done():
			return
		}
	}
}

// cleanup removes visitors that haven't made requests recently
func (rl *ipRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	for ip, v := range rl.visitors {
		// Remove if no requests in the last 2 windows
		if now.Sub(v.lastClean) > rl.cfg.window*2 {
			delete(rl.visitors, ip)
		}
	}
}

// allow checks if a request from the given IP should be allowed
func (rl *ipRateLimiter) allow(ip string) bool {
	if !rl.cfg.enabled {
		return true
	}
	
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	v, exists := rl.visitors[ip]
	if !exists {
		v = &visitor{
			requests:  []time.Time{now},
			lastClean: now,
		}
		rl.visitors[ip] = v
		return true
	}
	
	// Remove old requests outside the window
	cutoff := now.Add(-rl.cfg.window)
	filtered := make([]time.Time, 0, len(v.requests))
	for _, t := range v.requests {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	v.requests = filtered
	v.lastClean = now
	
	// Check if under limit
	if len(v.requests) >= rl.cfg.requestsPerIP {
		return false
	}
	
	// Allow request and record it
	v.requests = append(v.requests, now)
	return true
}

// rateLimitMiddleware applies rate limiting to sensitive endpoints
func rateLimitMiddleware(next http.Handler, limiter *ipRateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract IP from request (handle X-Forwarded-For for proxies)
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			// Take the first IP in the list (client IP)
			if idx := strings.Index(forwarded, ","); idx >= 0 {
				ip = strings.TrimSpace(forwarded[:idx])
			} else {
				ip = strings.TrimSpace(forwarded)
			}
		}
		// Strip port if present
		if idx := strings.LastIndex(ip, ":"); idx >= 0 {
			ip = ip[:idx]
		}
		
		if !limiter.allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too Many Requests - rate limit exceeded", http.StatusTooManyRequests)
			slog.Warn("rate limit exceeded", slog.String("ip", ip), slog.String("path", r.URL.Path))
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

// parseInt safely parses a string to int, returning default on error
func parseInt(s string, defaultVal int) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return defaultVal
	}
	return n
}

// corsConfig holds CORS configuration
type corsConfig struct {
	allowedOrigins []string
	permissive     bool // True for dev mode (allow all), false for production (restricted)
}

// loadCORSConfig reads CORS configuration from environment
func loadCORSConfig() *corsConfig {
	// Default to permissive in dev, restricted in production
	mode := strings.ToLower(os.Getenv("ENV"))
	permissive := mode == "" || mode == "dev" || mode == "development"
	
	// Allow explicit override
	if v := os.Getenv("CORS_PERMISSIVE"); v != "" {
		permissive = v == "1" || v == "true"
	}
	
	allowedOrigins := []string{}
	if origins := os.Getenv("CORS_ALLOWED_ORIGINS"); origins != "" {
		for _, origin := range strings.Split(origins, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				allowedOrigins = append(allowedOrigins, origin)
			}
		}
	}
	
	if !permissive && len(allowedOrigins) == 0 {
		slog.Warn("CORS restricted mode enabled but no CORS_ALLOWED_ORIGINS configured - all CORS requests will be blocked")
	}
	
	return &corsConfig{
		allowedOrigins: allowedOrigins,
		permissive:     permissive,
	}
}

// withCORSConfig wraps a handler with CORS headers based on configuration
func withCORSConfig(next http.Handler, cfg *corsConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		if cfg.permissive {
			// Dev mode: permissive CORS (allow all)
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token, X-Correlation-ID")
		} else {
			// Production mode: restricted CORS (allow only configured origins)
			if origin != "" && isOriginAllowed(origin, cfg.allowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token, X-Correlation-ID")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		
		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks if an origin is in the allowed list
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
		// Support wildcard subdomains (e.g., "*.example.com")
		if strings.HasPrefix(allowed, "*.") {
			domain := allowed[2:]
			if strings.HasSuffix(origin, "."+domain) || origin == "https://"+domain || origin == "http://"+domain {
				return true
			}
		}
	}
	return false
}

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const authContextKey contextKey = "authenticated"

// withAuthContext adds authentication status to request context
func withAuthContext(ctx context.Context, authenticated bool) context.Context {
	return context.WithValue(ctx, authContextKey, authenticated)
}

// isAuthenticated checks if the request is authenticated
func isAuthenticated(ctx context.Context) bool {
	if v, ok := ctx.Value(authContextKey).(bool); ok {
		return v
	}
	return false
}
