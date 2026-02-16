package server

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// handleChatJSON returns chat messages for a VOD within an optional time range.
func (h *Handlers) handleChatJSON(w http.ResponseWriter, r *http.Request, vodID string) {
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
		rows, err = h.db.QueryContext(r.Context(), `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=$1 AND rel_timestamp>=$2 AND rel_timestamp<=$3 ORDER BY rel_timestamp ASC LIMIT $4`, vodID, from, to, limit)
	} else {
		rows, err = h.db.QueryContext(r.Context(), `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=$1 AND rel_timestamp>=$2 ORDER BY rel_timestamp ASC LIMIT $3`, vodID, from, limit)
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
func (h *Handlers) handleChatSSE(w http.ResponseWriter, r *http.Request, vodID string) {
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
	rows, err := h.db.QueryContext(ctx, `SELECT username, message, abs_timestamp, rel_timestamp, badges, emotes, color FROM chat_messages WHERE vod_id=$1 AND rel_timestamp>=$2 ORDER BY rel_timestamp ASC`, vodID, from)
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
