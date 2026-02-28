package server

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	vodpkg "github.com/onnwee/vod-tender/backend/vod"
)

// HandleVodsList returns a paginated list of VODs.
func (h *Handlers) HandleVodsList(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.db.QueryContext(r.Context(), `
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
}

// HandleVodsDispatcher routes requests under /vods/{id}/* to appropriate sub-handlers.
func (h *Handlers) HandleVodsDispatcher(w http.ResponseWriter, r *http.Request) {
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
		h.handleVodDetail(w, r, vodID)
	case tail == "progress":
		h.handleVodProgress(w, r, vodID)
	case tail == "reprocess":
		h.handleVodReprocess(w, r, vodID)
	case tail == "cancel":
		h.handleVodCancel(w, r, vodID)
	case tail == "segments":
		h.handleVodSegments(w, r, vodID)
	case tail == "chat":
		h.handleChatJSON(w, r, vodID)
	case tail == "chat/stream":
		h.handleChatSSE(w, r, vodID)
	case tail == "description":
		h.handleVodDescription(w, r, vodID)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handlers) handleVodDetail(w http.ResponseWriter, r *http.Request, vodID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	row := h.db.QueryRowContext(r.Context(), `
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
	_ = h.db.QueryRowContext(r.Context(), `SELECT COALESCE(description,'') FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&v.Description)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleVodProgress returns a derived progress view with percent for the frontend.
func (h *Handlers) handleVodProgress(w http.ResponseWriter, r *http.Request, vodID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	row := h.db.QueryRowContext(r.Context(), `
        SELECT COALESCE(download_state, ''),
               COALESCE(download_retries, 0),
               COALESCE(download_total, 0),
               COALESCE(download_bytes, 0),
               COALESCE(downloaded_path, ''),
               COALESCE(processed, FALSE),
               COALESCE(youtube_url, ''),
               COALESCE(processing_error, ''),
               progress_updated_at
    FROM vods WHERE twitch_vod_id=$1
    `, vodID)
	var state, path, yt, processingError string
	var retries int
	var total int64
	var bytes int64
	var processed bool
	var updated *time.Time
	if err := row.Scan(&state, &retries, &total, &bytes, &path, &processed, &yt, &processingError, &updated); err != nil {
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
		"processing_error":    processingError,
		"youtube_url":         yt,
		"progress_updated_at": updated,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handleVodReprocess(w http.ResponseWriter, r *http.Request, vodID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, err := h.db.ExecContext(r.Context(), `
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
func (h *Handlers) handleVodCancel(w http.ResponseWriter, r *http.Request, vodID string) {
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
func (h *Handlers) handleVodSegments(w http.ResponseWriter, r *http.Request, _ string) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// handleVodDescription allows GET to read and PUT/PATCH to update the custom video description stored in DB.
func (h *Handlers) handleVodDescription(w http.ResponseWriter, r *http.Request, vodID string) {
	switch r.Method {
	case http.MethodGet:
		var desc string
		_ = h.db.QueryRowContext(r.Context(), `SELECT COALESCE(description,'') FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&desc)
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
		_, err := h.db.ExecContext(r.Context(), `UPDATE vods SET description=$1, updated_at=NOW() WHERE twitch_vod_id=$2`, strings.TrimSpace(body.Description), vodID)
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
