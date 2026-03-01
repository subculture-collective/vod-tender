package chat

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/onnwee/vod-tender/backend/testutil"
)

func TestLoadPrivacyPolicy(t *testing.T) {
	t.Setenv("CHAT_RETENTION_DAYS", "90")
	t.Setenv("CHAT_ANONYMIZE_AFTER_DAYS", "30")
	t.Setenv("CHAT_RETENTION_INTERVAL", "12h")
	t.Setenv("CHAT_ANONYMIZE_SALT", "test-salt")

	p := LoadPrivacyPolicy()

	if p.RetentionDays != 90 {
		t.Errorf("RetentionDays = %d, want 90", p.RetentionDays)
	}
	if p.AnonymizeAfterDays != 30 {
		t.Errorf("AnonymizeAfterDays = %d, want 30", p.AnonymizeAfterDays)
	}
	if p.Interval != 12*time.Hour {
		t.Errorf("Interval = %v, want 12h", p.Interval)
	}
	if p.AnonymizeSalt != "test-salt" {
		t.Errorf("AnonymizeSalt = %q, want test-salt", p.AnonymizeSalt)
	}
}

func TestRunChatPrivacyCycle_AnonymizesOldMessages(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	channel := "test_chat_privacy_anon"
	vodID := "test_chat_privacy_anon_vod"

	cleanupChatData(t, db, channel)
	insertTestVOD(t, ctx, db, channel, vodID)

	now := time.Now().UTC()
	insertChatMessage(t, ctx, db, channel, vodID, "OldUser", "old-message", now.Add(-45*24*time.Hour))
	insertChatMessage(t, ctx, db, channel, vodID, "RecentUser", "recent-message", now.Add(-3*24*time.Hour))

	policy := PrivacyPolicy{
		AnonymizeAfterDays: 30,
		AnonymizeSalt:      "privacy-salt",
		BatchSize:          10,
	}
	if err := runChatPrivacyCycle(ctx, db, channel, policy); err != nil {
		t.Fatalf("runChatPrivacyCycle() error = %v", err)
	}

	var oldUser string
	var oldHash sql.NullString
	var oldAnonymizedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT username, username_hash, anonymized_at
		FROM chat_messages
		WHERE channel = $1 AND message = 'old-message'
	`, channel).Scan(&oldUser, &oldHash, &oldAnonymizedAt); err != nil {
		t.Fatalf("query old message: %v", err)
	}

	if !strings.HasPrefix(oldUser, "anon_") {
		t.Errorf("old username = %q, want anon_* pseudonym", oldUser)
	}
	if !oldHash.Valid || oldHash.String == "" {
		t.Fatal("old message username_hash should be populated")
	}
	if !oldAnonymizedAt.Valid {
		t.Fatal("old message anonymized_at should be set")
	}

	expectedHash := UsernameHash("privacy-salt", "OldUser")
	if oldHash.String != expectedHash {
		t.Errorf("old username_hash = %q, want %q", oldHash.String, expectedHash)
	}

	var recentUser string
	var recentHash sql.NullString
	var recentAnonymizedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT username, username_hash, anonymized_at
		FROM chat_messages
		WHERE channel = $1 AND message = 'recent-message'
	`, channel).Scan(&recentUser, &recentHash, &recentAnonymizedAt); err != nil {
		t.Fatalf("query recent message: %v", err)
	}

	if recentUser != "RecentUser" {
		t.Errorf("recent username = %q, want RecentUser", recentUser)
	}
	if recentHash.Valid {
		t.Errorf("recent username_hash should be null, got %q", recentHash.String)
	}
	if recentAnonymizedAt.Valid {
		t.Errorf("recent anonymized_at should be null, got %v", recentAnonymizedAt.Time)
	}
}

func TestRunChatPrivacyCycle_DeletesExpiredMessages(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	channel := "test_chat_privacy_retention"
	vodID := "test_chat_privacy_retention_vod"

	cleanupChatData(t, db, channel)
	insertTestVOD(t, ctx, db, channel, vodID)

	now := time.Now().UTC()
	insertChatMessage(t, ctx, db, channel, vodID, "expiredUser", "expired-message", now.Add(-60*24*time.Hour))
	insertChatMessage(t, ctx, db, channel, vodID, "freshUser", "fresh-message", now.Add(-7*24*time.Hour))

	policy := PrivacyPolicy{
		RetentionDays: 30,
		BatchSize:     1,
	}
	if err := runChatPrivacyCycle(ctx, db, channel, policy); err != nil {
		t.Fatalf("runChatPrivacyCycle() error = %v", err)
	}

	var expiredCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel = $1 AND message = 'expired-message'`, channel).Scan(&expiredCount); err != nil {
		t.Fatalf("count expired: %v", err)
	}
	if expiredCount != 0 {
		t.Errorf("expired-message count = %d, want 0", expiredCount)
	}

	var freshCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel = $1 AND message = 'fresh-message'`, channel).Scan(&freshCount); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if freshCount != 1 {
		t.Errorf("fresh-message count = %d, want 1", freshCount)
	}
}

func TestAnonymizeOldMessages_Idempotent(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	channel := "test_chat_privacy_idempotent"
	vodID := "test_chat_privacy_idempotent_vod"

	cleanupChatData(t, db, channel)
	insertTestVOD(t, ctx, db, channel, vodID)

	insertChatMessage(t, ctx, db, channel, vodID, "IdempotentUser", "idempotent-message", time.Now().UTC().Add(-90*24*time.Hour))

	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	first, err := anonymizeOldMessages(ctx, db, channel, cutoff, "idempotent-salt", 10)
	if err != nil {
		t.Fatalf("first anonymizeOldMessages() error = %v", err)
	}
	second, err := anonymizeOldMessages(ctx, db, channel, cutoff, "idempotent-salt", 10)
	if err != nil {
		t.Fatalf("second anonymizeOldMessages() error = %v", err)
	}

	if first != 1 {
		t.Errorf("first anonymize affected = %d, want 1", first)
	}
	if second != 0 {
		t.Errorf("second anonymize affected = %d, want 0", second)
	}
}

func TestAnonymizeOldMessages_EmptySaltNoop(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	channel := "test_chat_privacy_empty_salt"
	vodID := "test_chat_privacy_empty_salt_vod"

	cleanupChatData(t, db, channel)
	insertTestVOD(t, ctx, db, channel, vodID)
	insertChatMessage(t, ctx, db, channel, vodID, "NoSaltUser", "no-salt-message", time.Now().UTC().Add(-60*24*time.Hour))

	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	affected, err := anonymizeOldMessages(ctx, db, channel, cutoff, "", 10)
	if err != nil {
		t.Fatalf("anonymizeOldMessages() error = %v", err)
	}
	if affected != 0 {
		t.Errorf("anonymize affected = %d, want 0", affected)
	}

	var username string
	var hash sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT username, username_hash
		FROM chat_messages
		WHERE channel = $1 AND message = 'no-salt-message'
	`, channel).Scan(&username, &hash); err != nil {
		t.Fatalf("query no-salt row: %v", err)
	}
	if username != "NoSaltUser" {
		t.Errorf("username = %q, want NoSaltUser", username)
	}
	if hash.Valid {
		t.Errorf("username_hash should remain null, got %q", hash.String)
	}
}

func TestRunChatPrivacyCycle_ChannelScoped(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	channelA := "test_chat_privacy_scope_a"
	channelB := "test_chat_privacy_scope_b"
	vodA := "test_chat_privacy_scope_a_vod"
	vodB := "test_chat_privacy_scope_b_vod"

	cleanupChatData(t, db, channelA)
	cleanupChatData(t, db, channelB)
	insertTestVOD(t, ctx, db, channelA, vodA)
	insertTestVOD(t, ctx, db, channelB, vodB)

	old := time.Now().UTC().Add(-120 * 24 * time.Hour)
	insertChatMessage(t, ctx, db, channelA, vodA, "UserA", "scope-a-message", old)
	insertChatMessage(t, ctx, db, channelB, vodB, "UserB", "scope-b-message", old)

	policy := PrivacyPolicy{RetentionDays: 30, BatchSize: 10}
	if err := runChatPrivacyCycle(ctx, db, channelA, policy); err != nil {
		t.Fatalf("runChatPrivacyCycle() error = %v", err)
	}

	var countA int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel = $1`, channelA).Scan(&countA); err != nil {
		t.Fatalf("count channel A: %v", err)
	}
	if countA != 0 {
		t.Errorf("channel A message count = %d, want 0", countA)
	}

	var countB int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages WHERE channel = $1`, channelB).Scan(&countB); err != nil {
		t.Fatalf("count channel B: %v", err)
	}
	if countB != 1 {
		t.Errorf("channel B message count = %d, want 1", countB)
	}
}

func cleanupChatData(t *testing.T, db *sql.DB, channel string) {
	t.Helper()
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, `DELETE FROM chat_messages WHERE channel = $1`, channel)
	_, _ = db.ExecContext(ctx, `DELETE FROM vods WHERE channel = $1`, channel)
}

func insertTestVOD(t *testing.T, ctx context.Context, db *sql.DB, channel, vodID string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, $2, $3, $4, 3600, NOW())
		ON CONFLICT (twitch_vod_id) DO UPDATE
		SET channel = EXCLUDED.channel,
		    title = EXCLUDED.title,
		    date = EXCLUDED.date,
		    duration_seconds = EXCLUDED.duration_seconds
	`, channel, vodID, "privacy test vod", time.Now().UTC())
	if err != nil {
		t.Fatalf("insert test vod: %v", err)
	}
}

func insertChatMessage(t *testing.T, ctx context.Context, db *sql.DB, channel, vodID, username, message string, abs time.Time) {
	t.Helper()
	rel := abs.Sub(abs.Add(-time.Second)).Seconds()
	_, err := db.ExecContext(ctx, `
		INSERT INTO chat_messages (channel, vod_id, username, message, abs_timestamp, rel_timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, channel, vodID, username, message, abs, rel)
	if err != nil {
		t.Fatalf("insert chat message: %v", err)
	}
}
