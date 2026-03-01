package chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultChatRetentionInterval = 6 * time.Hour
	defaultChatPrivacyBatchSize  = 500
)

// PrivacyPolicy defines configurable cleanup/anonymization behavior for chat messages.
type PrivacyPolicy struct {
	RetentionDays      int
	AnonymizeAfterDays int
	Interval           time.Duration
	BatchSize          int
	AnonymizeSalt      string
}

// LoadPrivacyPolicy loads chat privacy settings from environment variables.
//
// Supported variables:
//   - CHAT_RETENTION_DAYS
//   - CHAT_RETENTION_INTERVAL
//   - CHAT_ANONYMIZE_AFTER_DAYS
//   - CHAT_ANONYMIZE_SALT
func LoadPrivacyPolicy() PrivacyPolicy {
	policy := PrivacyPolicy{
		Interval:      defaultChatRetentionInterval,
		BatchSize:     defaultChatPrivacyBatchSize,
		AnonymizeSalt: strings.TrimSpace(os.Getenv("CHAT_ANONYMIZE_SALT")),
	}

	if n, ok := parseNonNegativeIntEnv("CHAT_RETENTION_DAYS"); ok {
		policy.RetentionDays = n
	}
	if n, ok := parseNonNegativeIntEnv("CHAT_ANONYMIZE_AFTER_DAYS"); ok {
		policy.AnonymizeAfterDays = n
	}
	if s := strings.TrimSpace(os.Getenv("CHAT_RETENTION_INTERVAL")); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			policy.Interval = d
		}
	}

	return policy
}

// StartChatPrivacyJob periodically applies chat anonymization and retention policies.
func StartChatPrivacyJob(ctx context.Context, dbx *sql.DB, channel string) {
	policy := LoadPrivacyPolicy()

	if policy.BatchSize <= 0 {
		policy.BatchSize = defaultChatPrivacyBatchSize
	}

	if policy.AnonymizeAfterDays > 0 && policy.AnonymizeSalt == "" {
		slog.Warn("chat privacy: anonymization disabled because CHAT_ANONYMIZE_SALT is empty", slog.String("channel", channel))
		policy.AnonymizeAfterDays = 0
	}

	if policy.RetentionDays <= 0 && policy.AnonymizeAfterDays <= 0 {
		slog.Info("chat privacy: job disabled (no retention or anonymization configured)", slog.String("channel", channel))
		return
	}

	slog.Info("chat privacy: job starting",
		slog.String("channel", channel),
		slog.Int("retention_days", policy.RetentionDays),
		slog.Int("anonymize_after_days", policy.AnonymizeAfterDays),
		slog.Duration("interval", policy.Interval),
		slog.Int("batch_size", policy.BatchSize),
	)

	if err := runChatPrivacyCycle(ctx, dbx, channel, policy); err != nil {
		slog.Warn("chat privacy: initial cleanup failed", slog.Any("err", err), slog.String("channel", channel))
	}

	ticker := time.NewTicker(policy.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runChatPrivacyCycle(ctx, dbx, channel, policy); err != nil {
				slog.Warn("chat privacy: cleanup cycle failed", slog.Any("err", err), slog.String("channel", channel))
			}
		}
	}
}

func runChatPrivacyCycle(ctx context.Context, dbx *sql.DB, channel string, policy PrivacyPolicy) error {
	now := time.Now().UTC()
	var (
		totalAnonymized int64
		totalDeleted    int64
	)

	if policy.AnonymizeAfterDays > 0 {
		cutoff := now.Add(-time.Duration(policy.AnonymizeAfterDays) * 24 * time.Hour)
		affected, err := anonymizeOldMessages(ctx, dbx, channel, cutoff, policy.AnonymizeSalt, policy.BatchSize)
		if err != nil {
			return err
		}
		totalAnonymized = affected
	}

	if policy.RetentionDays > 0 {
		cutoff := now.Add(-time.Duration(policy.RetentionDays) * 24 * time.Hour)
		affected, err := deleteExpiredMessages(ctx, dbx, channel, cutoff, policy.BatchSize)
		if err != nil {
			return err
		}
		totalDeleted = affected
	}

	if totalAnonymized > 0 || totalDeleted > 0 {
		slog.Info("chat privacy: cleanup cycle complete",
			slog.String("channel", channel),
			slog.Int64("anonymized", totalAnonymized),
			slog.Int64("deleted", totalDeleted),
		)
	}

	return nil
}

func anonymizeOldMessages(ctx context.Context, dbx *sql.DB, channel string, cutoff time.Time, salt string, batchSize int) (int64, error) {
	if strings.TrimSpace(salt) == "" {
		return 0, nil
	}

	if batchSize <= 0 {
		batchSize = defaultChatPrivacyBatchSize
	}

	var total int64
	for {
		rows, err := dbx.QueryContext(ctx, `
			SELECT id, username
			FROM chat_messages
			WHERE channel = $1
			  AND abs_timestamp IS NOT NULL
			  AND abs_timestamp < $2
			  AND username IS NOT NULL
			  AND username <> ''
			  AND username_hash IS NULL
			ORDER BY abs_timestamp ASC
			LIMIT $3
		`, channel, cutoff, batchSize)
		if err != nil {
			return total, err
		}

		type row struct {
			id       int64
			username string
		}
		batch := make([]row, 0, batchSize)
		for rows.Next() {
			var r row
			if scanErr := rows.Scan(&r.id, &r.username); scanErr != nil {
				_ = rows.Close()
				return total, scanErr
			}
			batch = append(batch, r)
		}
		if closeErr := rows.Close(); closeErr != nil {
			return total, closeErr
		}

		if len(batch) == 0 {
			return total, nil
		}

		for _, r := range batch {
			hash := UsernameHash(salt, r.username)
			if hash == "" {
				continue
			}
			pseudonym := UsernamePseudonym(hash)
			res, execErr := dbx.ExecContext(ctx, `
				UPDATE chat_messages
				SET username = $1,
				    username_hash = $2,
				    anonymized_at = NOW()
				WHERE id = $3 AND channel = $4 AND username_hash IS NULL
			`, pseudonym, hash, r.id, channel)
			if execErr != nil {
				return total, execErr
			}
			affected, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return total, rowsErr
			}
			total += affected
		}
	}
}

func deleteExpiredMessages(ctx context.Context, dbx *sql.DB, channel string, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = defaultChatPrivacyBatchSize
	}

	var total int64
	for {
		res, err := dbx.ExecContext(ctx, `
			DELETE FROM chat_messages
			WHERE id IN (
				SELECT id
				FROM chat_messages
				WHERE channel = $1
				  AND abs_timestamp IS NOT NULL
				  AND abs_timestamp < $2
				ORDER BY abs_timestamp ASC
				LIMIT $3
			)
		`, channel, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		affected, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return total, rowsErr
		}
		if affected == 0 {
			return total, nil
		}
		total += affected
	}
}

// UsernameHash deterministically hashes a username with a salt for privacy-safe matching.
func UsernameHash(salt, username string) string {
	s := strings.TrimSpace(salt)
	u := strings.ToLower(strings.TrimSpace(username))
	if s == "" || u == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s + ":" + u))
	return hex.EncodeToString(sum[:])
}

// UsernamePseudonym derives a stable anonymous display value from a hash.
func UsernamePseudonym(hash string) string {
	if len(hash) < 12 {
		return "anon"
	}
	return "anon_" + hash[:12]
}

func parseNonNegativeIntEnv(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
