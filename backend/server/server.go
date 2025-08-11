package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	vodpkg "github.com/onnwee/vod-tender/backend/vod"
)

// NewMux returns the HTTP handler with all routes.
func NewMux(db *sql.DB) http.Handler {
    mux := http.NewServeMux()

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
        rows, err := db.QueryContext(r.Context(), `SELECT twitch_vod_id, title, date, processed, youtube_url FROM vods ORDER BY date DESC LIMIT ? OFFSET ?`, limit, offset)
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
    return withCORS(mux)
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
        FROM vods WHERE twitch_vod_id=?
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
        FROM vods WHERE twitch_vod_id=?
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
        rows, err = db.QueryContext(r.Context(), `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=? AND rel_timestamp>=? AND rel_timestamp<=? ORDER BY rel_timestamp ASC LIMIT ?`, vodID, from, to, limit)
    } else {
        rows, err = db.QueryContext(r.Context(), `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=? AND rel_timestamp>=? ORDER BY rel_timestamp ASC LIMIT ?`, vodID, from, limit)
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
    rows, err := db.QueryContext(ctx, `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=? AND rel_timestamp>=? ORDER BY rel_timestamp ASC`, vodID, from)
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
        WHERE twitch_vod_id=?
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
