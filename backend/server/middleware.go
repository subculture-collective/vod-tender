package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// Rate limiter cleanup configuration
	// cleanupMultiplier determines how long to keep stale entries before cleanup.
	// Entries older than cleanupMultiplier * window are removed to prevent unbounded growth.
	cleanupMultiplier = 2

	// rateLimitDBTimeout is the maximum time to wait for database operations in the
	// distributed rate limiter. This prevents slow database queries from blocking requests.
	rateLimitDBTimeout = 2 * time.Second
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
	backend       string        // Backend type: "memory" or "postgres"
}

// loadRateLimiterConfig reads rate limiter configuration from environment
func loadRateLimiterConfig() *rateLimiterConfig {
	enabled := os.Getenv("RATE_LIMIT_ENABLED") != "0" // Enabled by default
	requestsPerIP := 10                               // Default: 10 requests per window
	window := 1 * time.Minute                         // Default: 1 minute window
	backend := os.Getenv("RATE_LIMIT_BACKEND")        // Backend: "memory" (default) or "postgres"

	// Default to memory backend for single-instance deployments
	if backend == "" {
		backend = "memory"
	}

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
		backend:       backend,
	}
}

// RateLimiter is an interface for rate limiting implementations
type RateLimiter interface {
	// allow checks if a request from the given IP should be allowed
	allow(ctx context.Context, ip string) bool
}

// ipRateLimiter implements a simple in-memory sliding window rate limiter per IP
type ipRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	cfg      *rateLimiterConfig
}

type visitor struct {
	requests  []time.Time
	lastClean time.Time
}

// newIPRateLimiter creates a new in-memory rate limiter
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
		// Remove if no requests in the last cleanupMultiplier windows
		if now.Sub(v.lastClean) > rl.cfg.window*cleanupMultiplier {
			delete(rl.visitors, ip)
		}
	}
}

// allow checks if a request from the given IP should be allowed
func (rl *ipRateLimiter) allow(ctx context.Context, ip string) bool {
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

// postgresRateLimiter implements a distributed rate limiter using Postgres
type postgresRateLimiter struct {
	db  *sql.DB
	cfg *rateLimiterConfig
}

// newPostgresRateLimiter creates a new Postgres-backed rate limiter
func newPostgresRateLimiter(ctx context.Context, db *sql.DB, cfg *rateLimiterConfig) (*postgresRateLimiter, error) {
	limiter := &postgresRateLimiter{
		db:  db,
		cfg: cfg,
	}

	// Table creation is handled by db.Migrate() in backend/db/db.go
	// Start cleanup goroutine to remove stale entries
	go limiter.cleanupLoop(ctx)

	return limiter, nil
}

// cleanupLoop periodically removes expired entries from the database
func (rl *postgresRateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// cleanup removes requests older than cleanupMultiplier * window
func (rl *postgresRateLimiter) cleanup(ctx context.Context) {
	cutoff := time.Now().Add(-rl.cfg.window * cleanupMultiplier)
	_, err := rl.db.ExecContext(ctx,
		`DELETE FROM rate_limit_requests WHERE request_time < $1`,
		cutoff)
	if err != nil {
		slog.Warn("rate limit cleanup failed", slog.Any("error", err))
	}
}

// allow checks if a request from the given IP should be allowed using Postgres
func (rl *postgresRateLimiter) allow(ctx context.Context, ip string) bool {
	if !rl.cfg.enabled {
		return true
	}

	ctx, cancel := context.WithTimeout(ctx, rateLimitDBTimeout)
	defer cancel()

	now := time.Now()
	cutoff := now.Add(-rl.cfg.window)

	// Use a transaction with advisory lock to ensure atomic check-and-insert
	tx, err := rl.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("rate limit: failed to begin transaction", slog.Any("error", err))
		// Fail open - allow request if DB is having issues
		return true
	}
	defer tx.Rollback()

	// Acquire advisory lock for this IP to ensure atomicity across concurrent requests
	// Using hashtext to convert IP string to integer for pg_advisory_xact_lock
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, ip)
	if err != nil {
		slog.Error("rate limit: failed to acquire advisory lock", slog.Any("error", err))
		// Fail open - allow request if lock acquisition fails
		return true
	}

	// Count recent requests within the window
	var count int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM rate_limit_requests 
		 WHERE ip = $1 AND request_time > $2`,
		ip, cutoff).Scan(&count)
	if err != nil {
		slog.Error("rate limit: failed to count requests", slog.Any("error", err))
		// Fail open - allow request if DB query fails
		return true
	}

	// If at or over limit, deny the request
	if count >= rl.cfg.requestsPerIP {
		return false
	}

	// Insert the new request
	_, err = tx.ExecContext(ctx,
		`INSERT INTO rate_limit_requests (ip, request_time) VALUES ($1, $2)`,
		ip, now)
	if err != nil {
		slog.Error("rate limit: failed to insert request", slog.Any("error", err))
		// Fail open - allow request if insert fails
		return true
	}

	// Commit the transaction (releases the advisory lock automatically)
	if err := tx.Commit(); err != nil {
		slog.Error("rate limit: failed to commit transaction", slog.Any("error", err))
		// Fail open - allow request if commit fails
		return true
	}

	return true
}

// rateLimitMiddleware applies rate limiting to sensitive endpoints
func rateLimitMiddleware(next http.Handler, limiter RateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract IP from request (handle X-Forwarded-For for proxies)
		// Use the rightmost IP before our trusted proxy to prevent client spoofing.
		// The leftmost IP can be set by the client, so it's untrustworthy.
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			// Use the last (rightmost) IP which is the one added by the closest proxy
			ip = strings.TrimSpace(parts[len(parts)-1])
		}
		// Strip port if present using net.SplitHostPort for IPv6 compatibility
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}

		if !limiter.allow(r.Context(), ip) {
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

// Note: authentication helpers removed as they were unused.
