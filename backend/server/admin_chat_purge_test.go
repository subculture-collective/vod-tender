package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	chatpkg "github.com/onnwee/vod-tender/backend/chat"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
)

func TestAdminChatUserPurgeEndpoint(t *testing.T) {
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

	t.Setenv("TWITCH_CHANNEL", "chat_purge_default")
	t.Setenv("CHAT_ANONYMIZE_SALT", "chat-purge-test-salt")

	mux := NewMux(context.Background(), db)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/chat/user/testuser", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})

	t.Run("missing username", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/chat/user/", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("delete plaintext username with default channel", func(t *testing.T) {
		ctx := context.Background()
		defaultChannel := "chat_purge_default"
		otherChannel := "chat_purge_other"
		vodDefault := "chat_purge_plain_default_vod"
		vodOther := "chat_purge_plain_other_vod"

		cleanupAdminChatTestData(t, db, defaultChannel)
		cleanupAdminChatTestData(t, db, otherChannel)

		insertAdminChatTestVOD(t, ctx, db, defaultChannel, vodDefault)
		insertAdminChatTestVOD(t, ctx, db, otherChannel, vodOther)

		insertAdminChatMessage(t, ctx, db, defaultChannel, vodDefault, "MixedCaseUser", "plain-target", nil)
		insertAdminChatMessage(t, ctx, db, otherChannel, vodOther, "MixedCaseUser", "plain-target-other-channel", nil)

		req := httptest.NewRequest(http.MethodDelete, "/admin/chat/user/mixedcaseuser", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["status"] != "ok" {
			t.Fatalf("unexpected response status: %v", resp)
		}

		var countDefault int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel=$1 AND message='plain-target'`, defaultChannel).Scan(&countDefault); err != nil {
			t.Fatalf("count default channel rows: %v", err)
		}
		if countDefault != 0 {
			t.Fatalf("expected default channel target rows deleted, got %d", countDefault)
		}

		var countOther int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel=$1 AND message='plain-target-other-channel'`, otherChannel).Scan(&countOther); err != nil {
			t.Fatalf("count other channel rows: %v", err)
		}
		if countOther != 1 {
			t.Fatalf("expected other channel rows unaffected, got %d", countOther)
		}
	})

	t.Run("delete anonymized rows by username hash", func(t *testing.T) {
		ctx := context.Background()
		channel := "chat_purge_hashed"
		vodID := "chat_purge_hashed_vod"
		username := "HashOnlyUser"
		hash := chatpkg.UsernameHash("chat-purge-test-salt", username)

		cleanupAdminChatTestData(t, db, channel)
		insertAdminChatTestVOD(t, ctx, db, channel, vodID)
		insertAdminChatMessage(t, ctx, db, channel, vodID, chatpkg.UsernamePseudonym(hash), "hashed-target", &hash)

		req := httptest.NewRequest(http.MethodDelete, "/admin/chat/user/HashOnlyUser?channel=chat_purge_hashed", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel=$1 AND message='hashed-target'`, channel).Scan(&count); err != nil {
			t.Fatalf("count hashed rows: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected hashed rows deleted, got %d", count)
		}
	})

	t.Run("channel query overrides default channel", func(t *testing.T) {
		ctx := context.Background()
		defaultChannel := "chat_purge_default"
		overrideChannel := "chat_purge_override"
		vodDefault := "chat_purge_scope_default_vod"
		vodOverride := "chat_purge_scope_override_vod"

		cleanupAdminChatTestData(t, db, defaultChannel)
		cleanupAdminChatTestData(t, db, overrideChannel)

		insertAdminChatTestVOD(t, ctx, db, defaultChannel, vodDefault)
		insertAdminChatTestVOD(t, ctx, db, overrideChannel, vodOverride)

		insertAdminChatMessage(t, ctx, db, defaultChannel, vodDefault, "ScopedUser", "scope-default", nil)
		insertAdminChatMessage(t, ctx, db, overrideChannel, vodOverride, "ScopedUser", "scope-override", nil)

		req := httptest.NewRequest(http.MethodDelete, "/admin/chat/user/ScopedUser?channel=chat_purge_override", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var countDefault int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel=$1 AND message='scope-default'`, defaultChannel).Scan(&countDefault); err != nil {
			t.Fatalf("count default rows: %v", err)
		}
		if countDefault != 1 {
			t.Fatalf("expected default channel row to remain, got %d", countDefault)
		}

		var countOverride int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel=$1 AND message='scope-override'`, overrideChannel).Scan(&countOverride); err != nil {
			t.Fatalf("count override rows: %v", err)
		}
		if countOverride != 0 {
			t.Fatalf("expected override channel row deleted, got %d", countOverride)
		}
	})
}

func TestAdminChatUserPurgeEndpointAuthProtection(t *testing.T) {
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

	t.Setenv("ADMIN_TOKEN", "chat-purge-admin-token")
	t.Setenv("TWITCH_CHANNEL", "chat_purge_auth")

	ctx := context.Background()
	channel := "chat_purge_auth"
	vodID := "chat_purge_auth_vod"
	cleanupAdminChatTestData(t, db, channel)
	insertAdminChatTestVOD(t, ctx, db, channel, vodID)
	insertAdminChatMessage(t, ctx, db, channel, vodID, "AuthUser", "auth-target", nil)

	mux := NewMux(context.Background(), db)

	unauthReq := httptest.NewRequest(http.MethodDelete, "/admin/chat/user/AuthUser", nil)
	unauthW := httptest.NewRecorder()
	mux.ServeHTTP(unauthW, unauthReq)
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", unauthW.Code)
	}

	authReq := httptest.NewRequest(http.MethodDelete, "/admin/chat/user/AuthUser", nil)
	authReq.Header.Set("X-Admin-Token", "chat-purge-admin-token")
	authW := httptest.NewRecorder()
	mux.ServeHTTP(authW, authReq)
	if authW.Code != http.StatusOK {
		t.Fatalf("expected 200 with auth, got %d: %s", authW.Code, authW.Body.String())
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel=$1 AND message='auth-target'`, channel).Scan(&count); err != nil {
		t.Fatalf("count auth-target rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected auth-target deleted, got %d", count)
	}
}

func cleanupAdminChatTestData(t *testing.T, db *sql.DB, channel string) {
	t.Helper()
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, `DELETE FROM chat_messages WHERE channel=$1`, channel)
	_, _ = db.ExecContext(ctx, `DELETE FROM vods WHERE channel=$1`, channel)
}

func insertAdminChatTestVOD(t *testing.T, ctx context.Context, db *sql.DB, channel, vodID string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, $2, $3, $4, 3600, NOW())
		ON CONFLICT (twitch_vod_id) DO UPDATE
		SET channel = EXCLUDED.channel,
		    title = EXCLUDED.title,
		    date = EXCLUDED.date,
		    duration_seconds = EXCLUDED.duration_seconds
	`, channel, vodID, "admin chat purge test vod", time.Now().UTC())
	if err != nil {
		t.Fatalf("insert test vod: %v", err)
	}
}

func insertAdminChatMessage(t *testing.T, ctx context.Context, db *sql.DB, channel, vodID, username, message string, usernameHash *string) {
	t.Helper()
	if usernameHash != nil {
		_, err := db.ExecContext(ctx, `
			INSERT INTO chat_messages (channel, vod_id, username, username_hash, message, abs_timestamp, rel_timestamp)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, channel, vodID, username, *usernameHash, message, time.Now().UTC(), 1.0)
		if err != nil {
			t.Fatalf("insert chat message with hash: %v", err)
		}
		return
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO chat_messages (channel, vod_id, username, message, abs_timestamp, rel_timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, channel, vodID, username, message, time.Now().UTC(), 1.0)
	if err != nil {
		t.Fatalf("insert chat message: %v", err)
	}
}
