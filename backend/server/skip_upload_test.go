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

func TestAdminVodSkipUploadEndpoint(t *testing.T) {
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
	vodID := "test_skip_upload_vod_123"
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, skip_upload, created_at)
		 VALUES ($1, $2, 'Test VOD', NOW(), 60, FALSE, NOW())
		 ON CONFLICT (twitch_vod_id) DO UPDATE SET skip_upload=FALSE`,
		channel, vodID)
	if err != nil {
		t.Fatal(err)
	}

	mux := NewMux(context.Background(), db)

	t.Run("enable skip_upload", func(t *testing.T) {
		reqBody := map[string]any{
			"vod_id":      vodID,
			"skip_upload": true,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/admin/vod/skip-upload", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var skipUpload bool
		err := db.QueryRowContext(context.Background(), `SELECT skip_upload FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&skipUpload)
		if err != nil {
			t.Fatal(err)
		}
		if !skipUpload {
			t.Fatal("expected skip_upload=true after endpoint call")
		}
	})

	t.Run("missing vod_id", func(t *testing.T) {
		reqBody := map[string]any{"skip_upload": true}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/admin/vod/skip-upload", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("nonexistent vod", func(t *testing.T) {
		reqBody := map[string]any{
			"vod_id":      "nonexistent_vod_999",
			"skip_upload": true,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/admin/vod/skip-upload", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}
