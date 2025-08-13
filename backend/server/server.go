package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/onnwee/vod-tender/backend/config"
	"github.com/onnwee/vod-tender/backend/telemetry"
	"github.com/onnwee/vod-tender/backend/twitchapi"
	vodpkg "github.com/onnwee/vod-tender/backend/vod"
	"github.com/onnwee/vod-tender/backend/youtubeapi"
)

// oauthTokenStore adapts the DB to youtubeapi.TokenStore interface
type oauthTokenStore struct { db *sql.DB }
func (o *oauthTokenStore) UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error {
    _, err := o.db.ExecContext(ctx, `INSERT INTO oauth_tokens(provider, access_token, refresh_token, expires_at, scope, updated_at) VALUES ($1,$2,$3,$4,$5,NOW())
        ON CONFLICT(provider) DO UPDATE SET access_token=EXCLUDED.access_token, refresh_token=EXCLUDED.refresh_token, expires_at=EXCLUDED.expires_at, updated_at=NOW()`, provider, accessToken, refreshToken, expiry, "")
    return err
}
func (o *oauthTokenStore) GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error) {
    row := o.db.QueryRowContext(ctx, `SELECT access_token, refresh_token, expires_at FROM oauth_tokens WHERE provider=$1`, provider)
    err = row.Scan(&accessToken, &refreshToken, &expiry)
    if err == sql.ErrNoRows { return "", "", time.Time{}, "", nil }
    return
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
        if _, err := rand.Read(b); err != nil { http.Error(w, "state gen error", 500); return }
        st := hex.EncodeToString(b)
        stateStore[st] = time.Now().Add(10 * time.Minute)
        authURL, err := twitchapi.BuildAuthorizeURL(cfg.TwitchClientID, cfg.TwitchRedirectURI, cfg.TwitchScopes, st)
        if err != nil { http.Error(w, err.Error(), 500); return }
        http.Redirect(w, r, authURL, http.StatusFound)
    })

    // Twitch OAuth callback (redirect_uri must point here). Expects code & state.
    mux.HandleFunc("/auth/twitch/callback", func(w http.ResponseWriter, r *http.Request) {
        cfg, _ := config.Load()
        code := r.URL.Query().Get("code")
        st := r.URL.Query().Get("state")
        if code == "" || st == "" { http.Error(w, "missing code/state", 400); return }
        // validate state
        exp, ok := stateStore[st]
        if !ok || time.Now().After(exp) { http.Error(w, "invalid state", 400); return }
        delete(stateStore, st)
        ctx := r.Context()
        res, err := twitchapi.ExchangeAuthCode(ctx, cfg.TwitchClientID, cfg.TwitchClientSecret, code, cfg.TwitchRedirectURI)
        if err != nil { http.Error(w, err.Error(), 500); return }
        // persist tokens
        _, err = db.Exec(`INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at, scope, updated_at) VALUES ($1,$2,$3,$4,$5,NOW())
            ON CONFLICT(provider) DO UPDATE SET access_token=EXCLUDED.access_token, refresh_token=EXCLUDED.refresh_token, expires_at=EXCLUDED.expires_at, scope=EXCLUDED.scope, updated_at=NOW()`,
            "twitch", res.AccessToken, res.RefreshToken, twitchapi.ComputeExpiry(res.ExpiresIn), strings.Join(res.Scope, " "))
        if err != nil { http.Error(w, err.Error(), 500); return }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{"status":"ok","scopes":res.Scope,"expires_in":res.ExpiresIn})
    })

    // YouTube OAuth start
    mux.HandleFunc("/auth/youtube/start", func(w http.ResponseWriter, r *http.Request) {
        cfg, _ := config.Load()
        if cfg.YTClientID == "" || cfg.YTRedirectURI == "" { http.Error(w, "youtube oauth not configured", 400); return }
        // generate state
        b := make([]byte, 16)
        if _, err := rand.Read(b); err != nil { http.Error(w, "state gen error", 500); return }
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
        if code == "" || st == "" { http.Error(w, "missing code/state", 400); return }
        exp, ok := stateStore[st]
        if !ok || time.Now().After(exp) { http.Error(w, "invalid state", 400); return }
        delete(stateStore, st)
        ts := &oauthTokenStore{db: db}
        yts := youtubeapi.New(cfg, ts)
        tok, err := yts.Exchange(r.Context(), code)
        if err != nil { http.Error(w, err.Error(), 500); return }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{"status":"ok","expiry":tok.Expiry,"access_token_present": tok.AccessToken != "","refresh_token_present": tok.RefreshToken != ""})
    })

    // Health
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        if err := db.PingContext(r.Context()); err != nil {
            http.Error(w, "unhealthy", http.StatusServiceUnavailable)
            return
        }
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })

    // Lightweight status summary (JSON)
    mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { http.Error(w, "method not allowed", http.StatusMethodNotAllowed); return }
        ctx := r.Context()
        resp := map[string]any{}
        // Queue depth & counts
        var pending, errored, processed int
        _ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE processed=0`).Scan(&pending)
        _ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE processed=0 AND processing_error IS NOT NULL AND processing_error!=''`).Scan(&errored)
        _ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE processed=1`).Scan(&processed)
        resp["pending"] = pending
        resp["errored"] = errored
        resp["processed"] = processed
        // Circuit breaker
        var cState, cFails, cUntil string
        _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&cState)
        _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_failures'`).Scan(&cFails)
        _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&cUntil)
        if cState != "" { resp["circuit_state"] = cState }
        if cFails != "" { resp["circuit_failures"] = cFails }
        if cUntil != "" { resp["circuit_open_until"] = cUntil }
        // Moving averages (ms)
        keys := []string{"avg_download_ms","avg_upload_ms","avg_total_ms"}
        for _, k := range keys {
            var v string
            _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=$1`, k).Scan(&v)
            if v != "" { resp[k] = v }
        }
        // Last job timestamp
        var last string
        _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='job_vod_process_last'`).Scan(&last)
        if last != "" { resp["last_process_run"] = last }
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
    rows, err := db.QueryContext(r.Context(), `SELECT twitch_vod_id, title, date, processed, youtube_url FROM vods ORDER BY date DESC LIMIT $1 OFFSET $2`, limit, offset)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer rows.Close()
        type vod struct {
            ID        string    `json:"id"`
            Title     string    `json:"title"`
            Date      time.Time `json:"date"`
            Processed bool      `json:"processed"`
            YouTube   string    `json:"youtube_url"`
        }
        var list []vod
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
        if err := vodpkg.DiscoverAndUpsert(ctx, db); err != nil {
            http.Error(w, err.Error(), 500)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
    })

    // Manual catalog backfill: /admin/vod/catalog?max=500&max_age_days=30
    mux.HandleFunc("/admin/vod/catalog", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost && r.Method != http.MethodGet {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }
        q := r.URL.Query()
        max := 0
        if s := q.Get("max"); s != "" { if n, err := strconv.Atoi(s); err == nil && n > 0 { max = n } }
        var maxAge time.Duration
        if s := q.Get("max_age_days"); s != "" { if n, err := strconv.Atoi(s); err == nil && n > 0 { maxAge = time.Duration(n) * 24 * time.Hour } }
        if err := vodpkg.BackfillCatalog(r.Context(), db, max, maxAge); err != nil {
            http.Error(w, err.Error(), 500)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "max": max})
    })

    // Monitoring summary endpoint
    mux.HandleFunc("/admin/monitor", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { http.Error(w, "method not allowed", http.StatusMethodNotAllowed); return }
        ctx := r.Context()
        // Fetch job timestamps
        keys := []string{"job_vod_process_last","job_vod_discovery_last","job_vod_backfill_last","job_vod_catalog_last"}
        stats := map[string]any{}
        for _, k := range keys {
            var val string
            row := db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=$1`, k)
            _ = row.Scan(&val)
            if val != "" { stats[k] = val }
        }
        // Circuit breaker
        var cState, cUntil, cFails string
        _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&cState)
        _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&cUntil)
        _ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_failures'`).Scan(&cFails)
        if cState != "" { stats["circuit_state"] = cState }
        if cUntil != "" { stats["circuit_open_until"] = cUntil }
        if cFails != "" { stats["circuit_failures"] = cFails }

        // Queue counts
        var pending, errored, processed int
        _ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE processed=0`).Scan(&pending)
        _ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE processed=0 AND processing_error IS NOT NULL AND processing_error!=''`).Scan(&errored)
        _ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE processed=1`).Scan(&processed)
        stats["vods_pending"] = pending
        stats["vods_errored"] = errored
        stats["vods_processed"] = processed
        // Oldest unprocessed
        var oldestID string
        var oldestDate time.Time
        row := db.QueryRowContext(ctx, `SELECT twitch_vod_id, date FROM vods WHERE processed=0 ORDER BY date ASC LIMIT 1`)
        _ = row.Scan(&oldestID, &oldestDate)
        if oldestID != "" { stats["oldest_pending"] = map[string]any{"id": oldestID, "date": oldestDate} }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(stats)
    })

    // Chat JSON and SSE under /vods/{id}/chat and /vods/{id}/chat/stream
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
        default:
            http.NotFound(w, r)
        }
    })
    // Wrap with correlation ID injector
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Reuse corr header if provided else generate
        corr := r.Header.Get("X-Correlation-ID")
        if corr == "" { corr = uuid.New().String() }
        ctx := telemetry.WithCorrelation(r.Context(), corr)
        w.Header().Set("X-Correlation-ID", corr)
        // Provide logger with corr for downstream if needed
        telemetry.LoggerWithCorr(ctx).Debug("request start", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("component", "http"))
        mux.ServeHTTP(w, r.WithContext(ctx))
    })
    return withCORS(handler)
}

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
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
        SELECT twitch_vod_id, title, date, duration_seconds, processed, youtube_url,
               downloaded_path, download_state, download_retries, download_total, progress_updated_at
    FROM vods WHERE twitch_vod_id=$1
    `, vodID)
    type vod struct {
        ID        string     `json:"id"`
        Title     string     `json:"title"`
        Date      time.Time  `json:"date"`
        Duration  int        `json:"duration_seconds"`
        Processed bool       `json:"processed"`
        YouTube   string     `json:"youtube_url"`
        DownloadedPath string `json:"downloaded_path"`
        DownloadState string  `json:"download_state"`
        DownloadRetries int   `json:"download_retries"`
        DownloadTotal   int64 `json:"download_total"`
        ProgressUpdated *time.Time `json:"progress_updated_at,omitempty"`
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
        SELECT download_state, download_retries, download_total, downloaded_path, processed, youtube_url, progress_updated_at
    FROM vods WHERE twitch_vod_id=$1
    `, vodID)
    var state, path, yt string
    var retries int
    var total int64
    var processed bool
    var updated *time.Time
    if err := row.Scan(&state, &retries, &total, &path, &processed, &yt, &updated); err != nil {
        if err == sql.ErrNoRows {
            http.NotFound(w, r)
            return
        }
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    percent := derivePercent(state)
    resp := map[string]any{
        "vod_id": vodID,
        "state": state,
        "percent": percent,
        "retries": retries,
        "total_bytes": total,
        "downloaded_path": path,
        "processed": processed,
        "youtube_url": yt,
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
    defer rows.Close()
    type msg struct {
        User  string    `json:"username"`
        Text  string    `json:"message"`
        Abs   time.Time `json:"abs_timestamp"`
        Rel   float64   `json:"rel_timestamp"`
        Badges string   `json:"badges"`
        Emotes string   `json:"emotes"`
        Color  string   `json:"color"`
    }
    var out []msg
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
    defer rows.Close()

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    type row struct {
        User  string
        Text  string
        Abs   time.Time
        Rel   float64
        Badges string
        Emotes string
        Color  string
    }
    var prev float64 = from
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
        w.Write([]byte("data: "))
        _ = enc.Encode(map[string]any{
            "username": m.User,
            "message":  m.Text,
            "abs_timestamp": m.Abs,
            "rel_timestamp": m.Rel,
            "badges": m.Badges,
            "emotes": m.Emotes,
            "color": m.Color,
        })
        w.Write([]byte("\n"))
        flusher.Flush()
        prev = m.Rel
    }
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
