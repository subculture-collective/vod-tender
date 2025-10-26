// Package server exposes the HTTP API: health, status, metrics, and VOD/chat helpers
// used by the frontend. It includes permissive CORS for development and injects
// correlation IDs into request contexts for consistent logging.
package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/onnwee/vod-tender/backend/config"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/telemetry"
	"github.com/onnwee/vod-tender/backend/twitchapi"
	vodpkg "github.com/onnwee/vod-tender/backend/vod"
	"github.com/onnwee/vod-tender/backend/youtubeapi"
)

// oauthTokenStore adapts the DB to youtubeapi.TokenStore interface
type oauthTokenStore struct{ db *sql.DB }

func (o *oauthTokenStore) UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error {
	// Use dbpkg.UpsertOAuthToken which handles encryption automatically
	return dbpkg.UpsertOAuthToken(ctx, o.db, provider, accessToken, refreshToken, expiry, raw, "")
}
func (o *oauthTokenStore) GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error) {
	// Use dbpkg.GetOAuthToken which handles decryption automatically
	access, refresh, exp, scope, dbErr := dbpkg.GetOAuthToken(ctx, o.db, provider)
	return access, refresh, exp, scope, dbErr
}

// NewMux returns the HTTP handler with all routes.
func NewMux(db *sql.DB) http.Handler {
	mux := http.NewServeMux()

	// Metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Simple in-memory state map for demo (not production-scale)
	stateStore := make(map[string]time.Time)

	// Twitch OAuth start (redirect user to Twitch)
	mux.HandleFunc("/auth/twitch/start", func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := config.Load() // ignore error; minimal usage
		if cfg.TwitchClientID == "" || cfg.TwitchRedirectURI == "" {
			http.Error(w, "oauth not configured (need TWITCH_CLIENT_ID + TWITCH_REDIRECT_URI)", http.StatusBadRequest)
			return
		}
		// generate state
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "state gen error", 500)
			return
		}
		st := hex.EncodeToString(b)
		stateStore[st] = time.Now().Add(10 * time.Minute)
		authURL, err := twitchapi.BuildAuthorizeURL(cfg.TwitchClientID, cfg.TwitchRedirectURI, cfg.TwitchScopes, st)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, authURL, http.StatusFound)
	})

	// Twitch OAuth callback (redirect_uri must point here). Expects code & state.
	mux.HandleFunc("/auth/twitch/callback", func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := config.Load()
		code := r.URL.Query().Get("code")
		st := r.URL.Query().Get("state")
		if code == "" || st == "" {
			http.Error(w, "missing code/state", 400)
			return
		}
		// validate state
		exp, ok := stateStore[st]
		if !ok || time.Now().After(exp) {
			http.Error(w, "invalid state", 400)
			return
		}
		delete(stateStore, st)
		ctx := r.Context()
		res, err := twitchapi.ExchangeAuthCode(ctx, cfg.TwitchClientID, cfg.TwitchClientSecret, code, cfg.TwitchRedirectURI)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// persist tokens using dbpkg.UpsertOAuthToken (handles encryption)
		dbErr := dbpkg.UpsertOAuthToken(ctx, db, "twitch", res.AccessToken, res.RefreshToken,
			twitchapi.ComputeExpiry(res.ExpiresIn), "", strings.Join(res.Scope, " "))
		if dbErr != nil {
			http.Error(w, dbErr.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "scopes": res.Scope, "expires_in": res.ExpiresIn}); err != nil {
			slog.Warn("failed to encode JSON response", slog.Any("err", err))
		}
	})

	// YouTube OAuth start
	mux.HandleFunc("/auth/youtube/start", func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := config.Load()
		if cfg.YTClientID == "" || cfg.YTRedirectURI == "" {
			http.Error(w, "youtube oauth not configured", 400)
			return
		}
		// generate state
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "state gen error", 500)
			return
		}
		st := hex.EncodeToString(b)
		stateStore[st] = time.Now().Add(10 * time.Minute)
		// Build auth URL manually (reuse youtubeapi oauth config)
		ts := &oauthTokenStore{db: db}
		yts := youtubeapi.New(cfg, ts)
		authURL := yts.AuthCodeURL(st)
		http.Redirect(w, r, authURL, http.StatusFound)
	})

	// YouTube OAuth callback
	mux.HandleFunc("/auth/youtube/callback", func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := config.Load()
		code := r.URL.Query().Get("code")
		st := r.URL.Query().Get("state")
		if code == "" || st == "" {
			http.Error(w, "missing code/state", 400)
			return
		}
		exp, ok := stateStore[st]
		if !ok || time.Now().After(exp) {
			http.Error(w, "invalid state", 400)
			return
		}
		delete(stateStore, st)
		ts := &oauthTokenStore{db: db}
		yts := youtubeapi.New(cfg, ts)
		tok, err := yts.Exchange(r.Context(), code)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "expiry": tok.Expiry, "access_token_present": tok.AccessToken != "", "refresh_token_present": tok.RefreshToken != ""}); err != nil {
			slog.Warn("failed to encode JSON response", slog.Any("err", err))
		}
	})

	// Health (liveness)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness (can handle traffic)
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checks := []struct {
			name string
			fn   func() error
		}{
			{"database", func() error { return db.PingContext(r.Context()) }},
			{"circuit_breaker", func() error {
				var state string
				err := db.QueryRowContext(r.Context(),
					"SELECT value FROM kv WHERE key='circuit_state'").Scan(&state)
				if err != nil && err != sql.ErrNoRows {
					return err
				}
				if state == "open" {
					return fmt.Errorf("circuit breaker open")
				}
				return nil
			}},
			{"credentials", func() error {
				var count int
				err := db.QueryRowContext(r.Context(),
					"SELECT COUNT(*) FROM oauth_tokens WHERE provider IN ('twitch', 'youtube')").Scan(&count)
				if err != nil {
					return err
				}
				if count < 1 {
					return fmt.Errorf("missing OAuth tokens")
				}
				return nil
			}},
		}

		for _, check := range checks {
			if err := check.fn(); err != nil {
				// Set headers before writing status code
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status":       "not_ready",
					"failed_check": check.name,
					"error":        err.Error(),
				})
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})

	// Config: safe editable settings via KV (whitelist)
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET/PUT for known keys; secrets must not be exposed here.
		safeKeys := map[string]bool{
			"LOG_LEVEL":                   true,
			"LOG_FORMAT":                  true,
			"DATA_DIR":                    true,
			"VOD_PROCESS_INTERVAL":        true,
			"PROCESSING_RETRY_COOLDOWN":   true,
			"DOWNLOAD_MAX_ATTEMPTS":       true,
			"DOWNLOAD_BACKOFF_BASE":       true,
			"UPLOAD_MAX_ATTEMPTS":         true,
			"UPLOAD_BACKOFF_BASE":         true,
			"RETAIN_KEEP_NEWER_THAN_DAYS": true,
			"BACKFILL_UPLOAD_DAILY_LIMIT": true,
		}
		switch r.Method {
		case http.MethodGet:
			// Return safe keys with values from env override (kv) if present
			out := map[string]string{}
			for k := range safeKeys {
				var v string
				_ = db.QueryRowContext(r.Context(), `SELECT value FROM kv WHERE key=$1`, "cfg:"+k).Scan(&v)
				if v == "" {
					v = os.Getenv(k)
				}
				if v != "" {
					out[k] = v
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPut:
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", 400)
				return
			}
			for k, v := range body {
				if !safeKeys[k] {
					continue
				}
				_, _ = db.ExecContext(r.Context(), `INSERT INTO kv (key,value,updated_at) VALUES ($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, "cfg:"+k, strings.TrimSpace(v))
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Lightweight status summary (JSON)
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		resp := map[string]any{}
		// Queue depth & counts
		var pending, errored, processed int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false`).Scan(&pending)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false AND processing_error IS NOT NULL AND processing_error!=''`).Scan(&errored)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=true`).Scan(&processed)
		resp["pending"] = pending
		resp["errored"] = errored
		resp["processed"] = processed
		// Circuit breaker
		var cState, cFails, cUntil string
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&cState)
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_failures'`).Scan(&cFails)
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&cUntil)
		if cState != "" {
			resp["circuit_state"] = cState
		}
		if cFails != "" {
			resp["circuit_failures"] = cFails
		}
		if cUntil != "" {
			resp["circuit_open_until"] = cUntil
		}
		// Moving averages (ms)
		keys := []string{"avg_download_ms", "avg_upload_ms", "avg_total_ms"}
		for _, k := range keys {
			var v string
			_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=$1`, k).Scan(&v)
			if v != "" {
				resp[k] = v
			}
		}
		// Last job timestamp
		var last string
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='job_vod_process_last'`).Scan(&last)
		if last != "" {
			resp["last_process_run"] = last
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// List VODs
	mux.HandleFunc("/vods", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Basic pagination: ?limit=50&offset=0
		limit := parseIntQuery(r, "limit", 50)
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		offset := parseIntQuery(r, "offset", 0)
		rows, err := db.QueryContext(r.Context(), `
        SELECT twitch_vod_id,
               COALESCE(title, ''),
               COALESCE(date, to_timestamp(0)),
               COALESCE(processed, FALSE),
               COALESCE(youtube_url, '')
        FROM vods
        ORDER BY COALESCE(date, to_timestamp(0)) DESC
        LIMIT $1 OFFSET $2
    `, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() {
			if err := rows.Close(); err != nil {
				slog.Warn("failed to close rows", slog.Any("err", err))
			}
		}()
		type vod struct {
			Date      time.Time `json:"date"`
			ID        string    `json:"id"`
			Title     string    `json:"title"`
			YouTube   string    `json:"youtube_url"`
			Processed bool      `json:"processed"`
		}
		list := make([]vod, 0)
		for rows.Next() {
			var v vod
			if err := rows.Scan(&v.ID, &v.Title, &v.Date, &v.Processed, &v.YouTube); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			list = append(list, v)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	})

	// Manual trigger: discover new VODs (no processing) - admin helper
	mux.HandleFunc("/admin/vod/scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		// Use channel from query param or default to TWITCH_CHANNEL env
		channel := r.URL.Query().Get("channel")
		if channel == "" {
			channel = os.Getenv("TWITCH_CHANNEL")
		}
		if err := vodpkg.DiscoverAndUpsert(ctx, db, channel); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "channel": channel})
	})

	// Manual catalog backfill: /admin/vod/catalog?max=500&max_age_days=30&channel=mychannel
	mux.HandleFunc("/admin/vod/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		// Use channel from query param or default to TWITCH_CHANNEL env
		channel := q.Get("channel")
		if channel == "" {
			channel = os.Getenv("TWITCH_CHANNEL")
		}
		max := 0
		if s := q.Get("max"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				max = n
			}
		}
		var maxAge time.Duration
		if s := q.Get("max_age_days"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				maxAge = time.Duration(n) * 24 * time.Hour
			}
		}
		if err := vodpkg.BackfillCatalog(r.Context(), db, channel, max, maxAge); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "max": max, "channel": channel})
	})

	// Monitoring summary endpoint
	mux.HandleFunc("/admin/monitor", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		// Fetch job timestamps
		keys := []string{"job_vod_process_last", "job_vod_discovery_last", "job_vod_backfill_last", "job_vod_catalog_last"}
		stats := map[string]any{}
		for _, k := range keys {
			var val string
			row := db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=$1`, k)
			_ = row.Scan(&val)
			if val != "" {
				stats[k] = val
			}
		}
		// Circuit breaker
		var cState, cUntil, cFails string
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&cState)
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&cUntil)
		_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_failures'`).Scan(&cFails)
		if cState != "" {
			stats["circuit_state"] = cState
		}
		if cUntil != "" {
			stats["circuit_open_until"] = cUntil
		}
		if cFails != "" {
			stats["circuit_failures"] = cFails
		}

		// Queue counts
		var pending, errored, processed int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false`).Scan(&pending)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false AND processing_error IS NOT NULL AND processing_error!=''`).Scan(&errored)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=true`).Scan(&processed)
		stats["vods_pending"] = pending
		stats["vods_errored"] = errored
		stats["vods_processed"] = processed
		// Oldest unprocessed
		var oldestID string
		var oldestDate time.Time
		row := db.QueryRowContext(ctx, `SELECT twitch_vod_id, date FROM vods WHERE COALESCE(processed,false)=false ORDER BY date ASC LIMIT 1`)
		_ = row.Scan(&oldestID, &oldestDate)
		if oldestID != "" {
			stats["oldest_pending"] = map[string]any{"id": oldestID, "date": oldestDate}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	})

	// Chat JSON/SSE under /vods/{id}/chat and /vods/{id}/chat/stream, plus import
	mux.HandleFunc("/vods/", func(w http.ResponseWriter, r *http.Request) {
		// crude routing
		path := strings.TrimPrefix(r.URL.Path, "/vods/")
		parts := strings.Split(path, "/")
		vodID := parts[0]
		tail := ""
		if len(parts) > 1 {
			tail = strings.Join(parts[1:], "/")
		}
		switch {
		case vodID == "" || vodID == "/":
			http.NotFound(w, r)
		case tail == "":
			handleVodDetail(w, r, db, vodID)
		case tail == "progress":
			handleVodProgress(w, r, db, vodID)
		case tail == "reprocess":
			handleVodReprocess(w, r, db, vodID)
		case tail == "cancel":
			handleVodCancel(w, r, db, vodID)
		case tail == "segments":
			handleVodSegments(w, r, db, vodID)
		case tail == "chat":
			handleChatJSON(w, r, db, vodID)
		case tail == "chat/stream":
			handleChatSSE(w, r, db, vodID)
		case tail == "chat/import":
			handleVodChatImport(w, r, db, vodID)
		case tail == "description":
			handleVodDescription(w, r, db, vodID)
		default:
			http.NotFound(w, r)
		}
	})

	// Admin convenience: /admin/vod/chat/import?id=<vodID>
	mux.HandleFunc("/admin/vod/chat/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		vodID := r.URL.Query().Get("id")
		if vodID == "" {
			http.Error(w, "missing id", 400)
			return
		}
		if err := vodpkg.ImportChat(r.Context(), db, vodID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "vod_id": vodID})
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
		mux.ServeHTTP(wrappedWriter, r.WithContext(ctx))

		// Record HTTP status in span
		telemetry.SetSpanHTTPStatus(span, wrappedWriter.statusCode)
		if wrappedWriter.statusCode >= 400 {
			code, msg := telemetry.ErrorStatus(fmt.Sprintf("HTTP %d", wrappedWriter.statusCode))
			span.SetStatus(code, msg)
		}
	})
	return withCORS(handler)
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

// cfgGet returns an override value from kv for a given key (with cfg: prefix) or falls back to env.
// Start runs the HTTP server and shuts down gracefully on context cancellation.
func Start(ctx context.Context, db *sql.DB, addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      NewMux(db),
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

// handleVodDetail returns a single VOD with useful fields for the frontend.
func handleVodDetail(w http.ResponseWriter, r *http.Request, db *sql.DB, vodID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	row := db.QueryRowContext(r.Context(), `
        SELECT twitch_vod_id,
               COALESCE(title, ''),
               COALESCE(date, to_timestamp(0)),
               COALESCE(duration_seconds, 0),
               COALESCE(processed, FALSE),
               COALESCE(youtube_url, ''),
               COALESCE(downloaded_path, ''),
               COALESCE(download_state, ''),
               COALESCE(download_retries, 0),
               COALESCE(download_total, 0),
               progress_updated_at
    FROM vods WHERE twitch_vod_id=$1
    `, vodID)
	type vod struct {
		Date            time.Time  `json:"date"`
		ProgressUpdated *time.Time `json:"progress_updated_at,omitempty"`
		ID              string     `json:"id"`
		Title           string     `json:"title"`
		YouTube         string     `json:"youtube_url"`
		DownloadedPath  string     `json:"downloaded_path"`
		DownloadState   string     `json:"download_state"`
		Description     string     `json:"description"`
		Duration        int        `json:"duration_seconds"`
		DownloadRetries int        `json:"download_retries"`
		DownloadTotal   int64      `json:"download_total"`
		Processed       bool       `json:"processed"`
	}
	var v vod
	if err := row.Scan(&v.ID, &v.Title, &v.Date, &v.Duration, &v.Processed, &v.YouTube,
		&v.DownloadedPath, &v.DownloadState, &v.DownloadRetries, &v.DownloadTotal, &v.ProgressUpdated); err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Load description separately to preserve compatibility with older schemas
	_ = db.QueryRowContext(r.Context(), `SELECT COALESCE(description,'') FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&v.Description)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleVodProgress returns a derived progress view with percent for the frontend.
func handleVodProgress(w http.ResponseWriter, r *http.Request, db *sql.DB, vodID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	row := db.QueryRowContext(r.Context(), `
        SELECT COALESCE(download_state, ''),
               COALESCE(download_retries, 0),
               COALESCE(download_total, 0),
               COALESCE(download_bytes, 0),
               COALESCE(downloaded_path, ''),
               COALESCE(processed, FALSE),
               COALESCE(youtube_url, ''),
               progress_updated_at
    FROM vods WHERE twitch_vod_id=$1
    `, vodID)
	var state, path, yt string
	var retries int
	var total int64
	var bytes int64
	var processed bool
	var updated *time.Time
	if err := row.Scan(&state, &retries, &total, &bytes, &path, &processed, &yt, &updated); err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Derive percent from state string; if absent, fall back to bytes/total.
	var percentVal float64
	if p := derivePercent(state); p != nil {
		percentVal = *p
	} else if total > 0 && bytes >= 0 {
		// Clamp to [0,100]
		percentVal = (float64(bytes) / float64(total)) * 100.0
		if percentVal < 0 {
			percentVal = 0
		}
		if percentVal > 100 {
			percentVal = 100
		}
	} else if processed || strings.EqualFold(state, "complete") {
		percentVal = 100
	} else {
		percentVal = 0
	}
	resp := map[string]any{
		"vod_id":              vodID,
		"state":               state,
		"percent":             percentVal,
		"retries":             retries,
		"total_bytes":         total,
		"downloaded_path":     path,
		"processed":           processed,
		"youtube_url":         yt,
		"progress_updated_at": updated,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleChatJSON returns chat messages for a VOD within an optional time range.
func handleChatJSON(w http.ResponseWriter, r *http.Request, db *sql.DB, vodID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Params: from, to (seconds), limit (default 1000)
	from := parseFloat64Query(r, "from", 0)
	to := parseFloat64Query(r, "to", 0)
	limit := parseIntQuery(r, "limit", 1000)
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	var rows *sql.Rows
	var err error
	if to > 0 {
		rows, err = db.QueryContext(r.Context(), `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=$1 AND rel_timestamp>=$2 AND rel_timestamp<=$3 ORDER BY rel_timestamp ASC LIMIT $4`, vodID, from, to, limit)
	} else {
		rows, err = db.QueryContext(r.Context(), `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=$1 AND rel_timestamp>=$2 ORDER BY rel_timestamp ASC LIMIT $3`, vodID, from, limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("failed to close rows", slog.Any("err", err))
		}
	}()
	type msg struct {
		Abs    time.Time `json:"abs_timestamp"`
		User   string    `json:"username"`
		Text   string    `json:"message"`
		Badges string    `json:"badges"`
		Emotes string    `json:"emotes"`
		Color  string    `json:"color"`
		Rel    float64   `json:"rel_timestamp"`
	}
	out := make([]msg, 0)
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.User, &m.Text, &m.Abs, &m.Rel, &m.Badges, &m.Emotes, &m.Color); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, m)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// handleChatSSE replays messages using Server-Sent Events at a given playback speed.
func handleChatSSE(w http.ResponseWriter, r *http.Request, db *sql.DB, vodID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	from := parseFloat64Query(r, "from", 0)
	speed := parseFloat64Query(r, "speed", 1.0)
	if speed <= 0 {
		speed = 1.0
	}
	ctx := r.Context()
	rows, err := db.QueryContext(ctx, `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=$1 AND rel_timestamp>=$2 ORDER BY rel_timestamp ASC`, vodID, from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("failed to close rows", slog.Any("err", err))
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	type row struct {
		Abs    time.Time
		User   string
		Text   string
		Badges string
		Emotes string
		Color  string
		Rel    float64
	}
	prev := from
	enc := json.NewEncoder(w)
	for rows.Next() {
		var m row
		if err := rows.Scan(&m.User, &m.Text, &m.Abs, &m.Rel, &m.Badges, &m.Emotes, &m.Color); err != nil {
			return
		}
		// sleep for the delta scaled by speed
		if m.Rel > prev {
			delay := time.Duration(((m.Rel - prev) / speed) * float64(time.Second))
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		// write SSE event
		if _, err := w.Write([]byte("data: ")); err != nil {
			slog.Warn("failed to write SSE data prefix", slog.Any("err", err))
			return
		}
		_ = enc.Encode(map[string]any{
			"username":      m.User,
			"message":       m.Text,
			"abs_timestamp": m.Abs,
			"rel_timestamp": m.Rel,
			"badges":        m.Badges,
			"emotes":        m.Emotes,
			"color":         m.Color,
		})
		if _, err := w.Write([]byte("\n")); err != nil {
			slog.Warn("failed to write SSE newline", slog.Any("err", err))
			return
		}
		flusher.Flush()
		prev = m.Rel
	}
}

// handleVodChatImport triggers chat replay import for a given VOD
func handleVodChatImport(w http.ResponseWriter, r *http.Request, db *sql.DB, vodID string) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if vodID == "" {
		http.Error(w, "missing vod id", http.StatusBadRequest)
		return
	}
	if err := vodpkg.ImportChat(r.Context(), db, vodID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "vod_id": vodID})
}

func parseFloat64Query(r *http.Request, key string, def float64) float64 {
	if v := r.URL.Query().Get(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func parseIntQuery(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// derivePercent extracts a float percent from yt-dlp progress string, if present.
func derivePercent(state string) *float64 {
	// example: "[download]   4.3% of ~2.19GiB at  3.05MiB/s ETA 11:22"
	i := strings.Index(state, "%")
	if i <= 0 {
		return nil
	}
	// walk backwards to find the number start
	j := i - 1
	for j >= 0 && (state[j] == '.' || (state[j] >= '0' && state[j] <= '9')) {
		j--
	}
	if j+1 >= i {
		return nil
	}
	num := state[j+1 : i]
	if f, err := strconv.ParseFloat(num, 64); err == nil {
		return &f
	}
	return nil
}

// withCORS wraps a handler to add permissive CORS headers suitable for local dev/SPA frontends.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleVodReprocess clears processing fields so the worker will re-download/re-upload on next cycle.
func handleVodReprocess(w http.ResponseWriter, r *http.Request, db *sql.DB, vodID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, err := db.ExecContext(r.Context(), `
        UPDATE vods
        SET processed=0,
            processing_error=NULL,
            youtube_url=NULL,
            downloaded_path=NULL,
            download_state=NULL,
            download_retries=0,
            download_bytes=0,
            download_total=0,
            progress_updated_at=NULL,
            updated_at=CURRENT_TIMESTAMP
        WHERE twitch_vod_id=$1
    `, vodID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVodCancel cancels an in-flight download if present.
func handleVodCancel(w http.ResponseWriter, r *http.Request, _ *sql.DB, vodID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if vodpkg.CancelDownload(vodID) {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	// No active download to cancel
	w.WriteHeader(http.StatusNoContent)
}

// handleVodSegments is a placeholder if future segmentation is added.
func handleVodSegments(w http.ResponseWriter, r *http.Request, _ *sql.DB, _ string) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// handleVodDescription allows GET to read and PUT/PATCH to update the custom video description stored in DB.
func handleVodDescription(w http.ResponseWriter, r *http.Request, db *sql.DB, vodID string) {
	switch r.Method {
	case http.MethodGet:
		var desc string
		_ = db.QueryRowContext(r.Context(), `SELECT COALESCE(description,'') FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&desc)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"vod_id": vodID, "description": desc})
		return
	case http.MethodPut, http.MethodPatch:
		var body struct {
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		_, err := db.ExecContext(r.Context(), `UPDATE vods SET description=$1, updated_at=NOW() WHERE twitch_vod_id=$2`, strings.TrimSpace(body.Description), vodID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}
