package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"

	vodpkg "github.com/onnwee/vod-tender/backend/vod"
)

// HandleConfig handles GET and PUT requests for safe configuration keys.
func (h *Handlers) HandleConfig(w http.ResponseWriter, r *http.Request) {
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
		"RETENTION_KEEP_DAYS":         true,
		"RETENTION_KEEP_COUNT":        true,
		"RETENTION_DRY_RUN":           true,
		"RETENTION_INTERVAL":          true,
		"BACKFILL_UPLOAD_DAILY_LIMIT": true,
	}
	switch r.Method {
	case http.MethodGet:
		// Return safe keys with values from env override (kv) if present
		out := map[string]string{}
		for k := range safeKeys {
			var v string
			_ = h.db.QueryRowContext(r.Context(), `SELECT value FROM kv WHERE key=$1`, "cfg:"+k).Scan(&v)
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
			if _, err := h.db.ExecContext(
				r.Context(),
				`INSERT INTO kv (key,value,updated_at) VALUES ($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`,
				"cfg:"+k,
				strings.TrimSpace(v),
			); err != nil {
				slog.Error("failed to update config", slog.String("key", k), slog.Any("err", err))
				http.Error(w, "failed to update config", http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleStatus returns a lightweight status summary including queue depth, circuit breaker state, etc.
func (h *Handlers) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	resp := map[string]any{}
	// Queue depth & counts
	var pending, errored, processed int
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false`).Scan(&pending)
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false AND processing_error IS NOT NULL AND processing_error!=''`).Scan(&errored)
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=true`).Scan(&processed)
	resp["pending"] = pending
	resp["errored"] = errored
	resp["processed"] = processed

	// Queue depth by priority (breakdown)
	type priorityCount struct {
		Priority int `json:"priority"`
		Count    int `json:"count"`
	}
	var priorityCounts []priorityCount
	rows, err := h.db.QueryContext(ctx, `
		SELECT COALESCE(priority, 0) as priority, COUNT(*) as count 
		FROM vods 
		WHERE COALESCE(processed,false)=false 
		GROUP BY priority 
		ORDER BY priority DESC
	`)
	if err == nil {
		defer func() {
			if err := rows.Close(); err != nil {
				slog.Warn("failed to close rows", slog.Any("err", err))
			}
		}()
		for rows.Next() {
			var pc priorityCount
			if err := rows.Scan(&pc.Priority, &pc.Count); err == nil {
				priorityCounts = append(priorityCounts, pc)
			}
		}
	}
	if len(priorityCounts) > 0 {
		resp["queue_by_priority"] = priorityCounts
	}

	// Download concurrency stats
	resp["active_downloads"] = vodpkg.GetActiveDownloads()
	resp["max_concurrent_downloads"] = vodpkg.GetMaxConcurrentDownloads()

	// Retry/backoff configuration
	retryConfig := map[string]any{
		"download_max_attempts":     getEnvInt("DOWNLOAD_MAX_ATTEMPTS", 5),
		"download_backoff_base":     os.Getenv("DOWNLOAD_BACKOFF_BASE"),
		"upload_max_attempts":       getEnvInt("UPLOAD_MAX_ATTEMPTS", 5),
		"upload_backoff_base":       os.Getenv("UPLOAD_BACKOFF_BASE"),
		"processing_retry_cooldown": os.Getenv("PROCESSING_RETRY_COOLDOWN"),
	}
	if retryConfig["download_backoff_base"] == "" {
		retryConfig["download_backoff_base"] = "2s"
	}
	if retryConfig["upload_backoff_base"] == "" {
		retryConfig["upload_backoff_base"] = "2s"
	}
	if retryConfig["processing_retry_cooldown"] == "" {
		retryConfig["processing_retry_cooldown"] = "600s"
	}
	resp["retry_config"] = retryConfig

	// Bandwidth limit if configured
	if limit := os.Getenv("DOWNLOAD_RATE_LIMIT"); limit != "" {
		resp["download_rate_limit"] = limit
	}

	// Circuit breaker
	var cState, cFails, cUntil string
	_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&cState)
	_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_failures'`).Scan(&cFails)
	_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&cUntil)
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
		_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=$1`, k).Scan(&v)
		if v != "" {
			resp[k] = v
		}
	}
	// Last job timestamp
	var last string
	_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='job_vod_process_last'`).Scan(&last)
	if last != "" {
		resp["last_process_run"] = last
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
