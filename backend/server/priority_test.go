package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

func TestAdminVodPriorityEndpoint(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()

	if err := dbpkg.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	// Insert test VOD
	channel := ""
	vodID := "test_priority_vod_123"
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, priority, created_at) 
		VALUES ($1, $2, 'Test VOD', NOW(), 60, 0, NOW()) 
		ON CONFLICT (twitch_vod_id) DO UPDATE SET priority=0`,
		channel, vodID)
	if err != nil {
		t.Fatal(err)
	}

	mux := NewMux(db)

	t.Run("update priority", func(t *testing.T) {
		reqBody := map[string]any{
			"vod_id":   vodID,
			"priority": 50,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/admin/vod/priority", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}

		if resp["status"] != "ok" {
			t.Fatalf("expected status ok, got %v", resp)
		}

		// Verify database update
		var priority int
		err := db.QueryRowContext(context.Background(),
			`SELECT priority FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&priority)
		if err != nil {
			t.Fatal(err)
		}

		if priority != 50 {
			t.Fatalf("expected priority 50, got %d", priority)
		}
	})

	t.Run("missing vod_id", func(t *testing.T) {
		reqBody := map[string]any{
			"priority": 10,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/admin/vod/priority", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("nonexistent vod", func(t *testing.T) {
		reqBody := map[string]any{
			"vod_id":   "nonexistent_vod_999",
			"priority": 10,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/admin/vod/priority", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("negative priority", func(t *testing.T) {
		reqBody := map[string]any{
			"vod_id":   vodID,
			"priority": -10,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/admin/vod/priority", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 (negative priority valid), got %d", w.Code)
		}

		// Verify database update
		var priority int
		err := db.QueryRowContext(context.Background(),
			`SELECT priority FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&priority)
		if err != nil {
			t.Fatal(err)
		}

		if priority != -10 {
			t.Fatalf("expected priority -10, got %d", priority)
		}
	})
}

func TestStatusEndpointEnhancements(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	}()

	if err := dbpkg.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	channel := ""
	// Insert test VODs with different priorities
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, priority, processed, created_at) 
		VALUES ($1, 'vod_p10', 'High Priority', NOW(), 60, 10, false, NOW()) 
		ON CONFLICT (twitch_vod_id) DO UPDATE SET priority=10, processed=false`,
		channel)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, priority, processed, created_at) 
		VALUES ($1, 'vod_p5_1', 'Med Priority 1', NOW(), 60, 5, false, NOW()) 
		ON CONFLICT (twitch_vod_id) DO UPDATE SET priority=5, processed=false`,
		channel)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, priority, processed, created_at) 
		VALUES ($1, 'vod_p5_2', 'Med Priority 2', NOW(), 60, 5, false, NOW()) 
		ON CONFLICT (twitch_vod_id) DO UPDATE SET priority=5, processed=false`,
		channel)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, priority, processed, created_at) 
		VALUES ($1, 'vod_p0', 'Default Priority', NOW(), 60, 0, false, NOW()) 
		ON CONFLICT (twitch_vod_id) DO UPDATE SET priority=0, processed=false`,
		channel)

	mux := NewMux(db)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	// Check for new fields
	if _, ok := resp["queue_by_priority"]; !ok {
		t.Error("expected queue_by_priority field in response")
	}

	if _, ok := resp["active_downloads"]; !ok {
		t.Error("expected active_downloads field in response")
	}

	if _, ok := resp["max_concurrent_downloads"]; !ok {
		t.Error("expected max_concurrent_downloads field in response")
	}

	if _, ok := resp["retry_config"]; !ok {
		t.Error("expected retry_config field in response")
	}

	// Verify queue_by_priority structure
	if queueByPriority, ok := resp["queue_by_priority"].([]any); ok {
		if len(queueByPriority) < 1 {
			t.Error("expected at least one priority level in queue_by_priority")
		}
	} else {
		t.Errorf("queue_by_priority is not an array: %T", resp["queue_by_priority"])
	}
}
