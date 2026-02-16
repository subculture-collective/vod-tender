// Package server exposes the HTTP API: health, status, metrics, and VOD/chat helpers
// used by the frontend. It includes permissive CORS for development and injects
// correlation IDs into request contexts for consistent logging.
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/onnwee/vod-tender/backend/telemetry"
)

// getVodSensitiveEndpointPattern returns a compiled regex pattern to match VOD-specific endpoints requiring rate limiting.
// Matches paths like /vods/{id}/cancel and /vods/{id}/reprocess.
// The pattern is lazily compiled on first use to reduce startup overhead.
var getVodSensitiveEndpointPattern = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^/vods/[^/]+/(cancel|reprocess)$`)
})

// NewMux returns the HTTP handler with all routes.
// The provided context is used for rate limiter cleanup goroutines lifecycle.
func NewMux(ctx context.Context, db *sql.DB) http.Handler {
	// Load middleware configurations
	authCfg := loadAuthConfig()
	rateLimiterCfg := loadRateLimiterConfig()
	corsCfg := loadCORSConfig()
	
	// Create rate limiter based on configuration
	var rateLimiter RateLimiter
	if rateLimiterCfg.backend == "postgres" {
		slog.Info("initializing distributed rate limiter", slog.String("backend", "postgres"))
		pgLimiter, err := newPostgresRateLimiter(ctx, db, rateLimiterCfg)
		if err != nil {
			slog.Error("failed to create postgres rate limiter, falling back to memory", slog.Any("error", err))
			rateLimiter = newIPRateLimiter(ctx, rateLimiterCfg)
		} else {
			rateLimiter = pgLimiter
		}
	} else {
		slog.Info("initializing in-memory rate limiter", slog.String("backend", "memory"))
		rateLimiter = newIPRateLimiter(ctx, rateLimiterCfg)
	}

	// Initialize handlers with dependencies
	handlers := NewHandlers(ctx, db)

	mux := http.NewServeMux()

	// Metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// OAuth endpoints
	mux.HandleFunc("/auth/twitch/start", handlers.HandleTwitchOAuthStart)
	mux.HandleFunc("/auth/twitch/callback", handlers.HandleTwitchOAuthCallback)
	mux.HandleFunc("/auth/youtube/start", handlers.HandleYouTubeOAuthStart)
	mux.HandleFunc("/auth/youtube/callback", handlers.HandleYouTubeOAuthCallback)

	// Health and readiness endpoints
	mux.HandleFunc("/healthz", handlers.HandleHealthz)
	mux.HandleFunc("/readyz", handlers.HandleReadyz)

	// Config and status endpoints
	mux.HandleFunc("/config", handlers.HandleConfig)
	mux.HandleFunc("/status", handlers.HandleStatus)

	// VOD endpoints
	mux.HandleFunc("/vods", handlers.HandleVodsList)
	mux.HandleFunc("/vods/", handlers.HandleVodsDispatcher)

	// Admin endpoints
	mux.HandleFunc("/admin/vod/scan", handlers.HandleAdminVodScan)
	mux.HandleFunc("/admin/vod/catalog", handlers.HandleAdminVodCatalog)
	mux.HandleFunc("/admin/monitor", handlers.HandleAdminMonitor)
	mux.HandleFunc("/admin/vod/priority", handlers.HandleAdminVodPriority)

	// Create a selective middleware wrapper that applies auth and rate limiting to admin endpoints
	selectiveHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Apply auth and rate limiting to admin endpoints
		if strings.HasPrefix(r.URL.Path, "/admin/") {
			// Apply auth first, then rate limiting
			adminAuth(rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mux.ServeHTTP(w, r)
			}), rateLimiter), authCfg).ServeHTTP(w, r)
			return
		}

		// Apply rate limiting to sensitive VOD operations (cancel, reprocess)
		// Using regex to ensure only /vods/{id}/cancel and /vods/{id}/reprocess are matched
		if getVodSensitiveEndpointPattern().MatchString(r.URL.Path) {
			rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mux.ServeHTTP(w, r)
			}), rateLimiter).ServeHTTP(w, r)
			return
		}

		// All other endpoints: no special protection
		mux.ServeHTTP(w, r)
	})

	// Wrap with correlation ID injector and tracing middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reuse corr header if provided else generate
		corr := r.Header.Get("X-Correlation-ID")
		if corr == "" {
			corr = uuid.New().String()
		}
		ctx := telemetry.WithCorrelation(r.Context(), corr)
		w.Header().Set("X-Correlation-ID", corr)

		// Start tracing span if enabled
		ctx, span := telemetry.StartSpan(ctx, "http-server", r.Method+" "+r.URL.Path,
			telemetry.HTTPMethodAttr(r.Method),
			telemetry.HTTPRouteAttr(r.URL.Path),
			telemetry.HTTPURLAttr(r.URL.String()),
		)
		defer span.End()

		// Provide logger with corr for downstream if needed
		telemetry.LoggerWithCorr(ctx).Debug("request start", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("component", "http"))

		// Capture status code via custom ResponseWriter
		wrappedWriter := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		selectiveHandler.ServeHTTP(wrappedWriter, r.WithContext(ctx))

		// Record HTTP status in span
		telemetry.SetSpanHTTPStatus(span, wrappedWriter.statusCode)
		if wrappedWriter.statusCode >= 400 {
			code, msg := telemetry.ErrorStatus(fmt.Sprintf("HTTP %d", wrappedWriter.statusCode))
			span.SetStatus(code, msg)
		}
	})
	return withCORSConfig(handler, corsCfg)
}

// statusRecorder wraps ResponseWriter to capture status code
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Flush implements http.Flusher if the underlying ResponseWriter supports it
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// cfgGet returns an override value from kv for a given key (with cfg: prefix) or falls back to env.
// Start runs the HTTP server and shuts down gracefully on context cancellation.
func Start(ctx context.Context, db *sql.DB, addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      NewMux(ctx, db),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Shutdown goroutine
	go func() {
		<-ctx.Done()
		// Use WithoutCancel to inherit context values but allow shutdown to complete
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("http server shutdown error", slog.Any("err", err))
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("http server error", slog.Any("err", err))
		return err
	}
	return nil
}

