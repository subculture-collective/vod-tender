package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
)

// HandleHealthz responds to liveness probe requests by checking database connectivity.
func (h *Handlers) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := h.db.PingContext(r.Context()); err != nil {
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// HandleReadyz responds to readiness probe requests with detailed system checks.
func (h *Handlers) HandleReadyz(w http.ResponseWriter, r *http.Request) {
	checks := []struct {
		name string
		fn   func() error
	}{
		{"database", func() error { return h.db.PingContext(r.Context()) }},
		{"circuit_breaker", func() error {
			var state string
			err := h.db.QueryRowContext(r.Context(),
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
			err := h.db.QueryRowContext(r.Context(),
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
}
