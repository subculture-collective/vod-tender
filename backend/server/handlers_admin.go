package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	vodpkg "github.com/onnwee/vod-tender/backend/vod"
)

// HandleAdminVodScan handles manual VOD discovery for a specific channel.
func (h *Handlers) HandleAdminVodScan(w http.ResponseWriter, r *http.Request) {
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
	if err := vodpkg.DiscoverAndUpsert(ctx, h.db, channel); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "channel": channel})
}

// HandleAdminVodCatalog handles manual catalog backfill with optional parameters.
func (h *Handlers) HandleAdminVodCatalog(w http.ResponseWriter, r *http.Request) {
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
	if err := vodpkg.BackfillCatalog(r.Context(), h.db, channel, max, maxAge); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "max": max, "channel": channel})
}

// HandleAdminMonitor returns monitoring summary including job timestamps and queue stats.
func (h *Handlers) HandleAdminMonitor(w http.ResponseWriter, r *http.Request) {
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
		row := h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=$1`, k)
		_ = row.Scan(&val)
		if val != "" {
			stats[k] = val
		}
	}
	// Circuit breaker
	var cState, cUntil, cFails string
	_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&cState)
	_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_open_until'`).Scan(&cUntil)
	_ = h.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_failures'`).Scan(&cFails)
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
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false`).Scan(&pending)
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=false AND processing_error IS NOT NULL AND processing_error!=''`).Scan(&errored)
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vods WHERE COALESCE(processed,false)=true`).Scan(&processed)
	stats["vods_pending"] = pending
	stats["vods_errored"] = errored
	stats["vods_processed"] = processed
	// Oldest unprocessed
	var oldestID string
	var oldestDate time.Time
	row := h.db.QueryRowContext(ctx, `SELECT twitch_vod_id, date FROM vods WHERE COALESCE(processed,false)=false ORDER BY date ASC LIMIT 1`)
	_ = row.Scan(&oldestID, &oldestDate)
	if oldestID != "" {
		stats["oldest_pending"] = map[string]any{"id": oldestID, "date": oldestDate}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// HandleAdminVodPriority handles VOD priority updates.
func (h *Handlers) HandleAdminVodPriority(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		VodID    string `json:"vod_id"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.VodID == "" {
		http.Error(w, "vod_id required", http.StatusBadRequest)
		return
	}

	// Update priority in database
	result, err := h.db.ExecContext(r.Context(),
		`UPDATE vods SET priority=$1, updated_at=NOW() WHERE twitch_vod_id=$2`,
		req.Priority, req.VodID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "vod not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"vod_id":   req.VodID,
		"priority": req.Priority,
	})
}

// HandleAdminVodSkipUpload toggles per-VOD upload skipping.
func (h *Handlers) HandleAdminVodSkipUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		VodID      string `json:"vod_id"`
		SkipUpload bool   `json:"skip_upload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.VodID == "" {
		http.Error(w, "vod_id required", http.StatusBadRequest)
		return
	}

	result, err := h.db.ExecContext(r.Context(),
		`UPDATE vods SET skip_upload=$1, updated_at=NOW() WHERE twitch_vod_id=$2`,
		req.SkipUpload, req.VodID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "vod not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"vod_id":      req.VodID,
		"skip_upload": req.SkipUpload,
	})
}
