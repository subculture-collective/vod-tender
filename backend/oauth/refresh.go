// Package oauth provides generic token refresh scheduling for providers whose
// tokens are persisted in the oauth_tokens table. It performs jittered checks
// and refreshes when expiry falls within a configured window.
package oauth

import (
	"context"
	"database/sql"
	"log/slog"
	"math/rand"
	"strings"
	"time"
)

// RefreshFunc performs provider-specific refresh and returns (access, refresh, expiry, scope)
type RefreshFunc func(ctx context.Context, refreshToken string) (string, string, time.Time, string, error)

// StartRefresher launches a goroutine that periodically checks an oauth token row and refreshes it.
// provider: key in oauth_tokens table.
// interval: how often to wake up and check.
// window: refresh when remaining lifetime <= window.
func StartRefresher(ctx context.Context, db *sql.DB, provider string, interval, window time.Duration, fn RefreshFunc) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	// Randomize initial delay to spread load across instances.
	//nolint:gosec // G404: math/rand is sufficient for scheduling jitter, not used for security
	initialJitter := time.Duration(rand.Int63n(int64(interval / 2)))
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(initialJitter):
		}
		for {
			// Add per-iteration jitter (±20% of interval) for scheduling diversity.
			jitterRange := int64(interval / 5)
			//nolint:gosec // G404: math/rand is sufficient for scheduling jitter, not used for security
			jitter := time.Duration(rand.Int63n(jitterRange*2) - jitterRange)
			nextSleep := interval + jitter
			if nextSleep < interval/2 {
				nextSleep = interval / 2
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(nextSleep):
			}
			row := db.QueryRowContext(ctx, `SELECT access_token, refresh_token, expires_at, scope FROM oauth_tokens WHERE provider=$1 LIMIT 1`, provider)
			var at, rt, scope string
			var exp time.Time
			if err := row.Scan(&at, &rt, &exp, &scope); err != nil {
				continue
			}
			if rt == "" {
				continue
			}
			// If still outside window skip quickly
			if time.Until(exp) > window {
				continue
			}
			// Small pre-refresh jitter to avoid stampedes when many pods see same expiry
			//nolint:gosec // G404: math/rand is sufficient for jitter, not used for security
			pre := time.Duration(rand.Int63n(int64(5 * time.Second)))
			select {
				case <-ctx.Done():
					return
				case <-time.After(pre):
			}
			ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
			newAT, newRT, newExp, newScope, err := fn(ctx2, rt)
			cancel()
			if err != nil {
				slog.Warn("token refresh failed", slog.String("provider", provider), slog.Any("err", err))
				continue
			}
			if newRT == "" {
				newRT = rt
			}
			if newScope == "" {
				newScope = scope
			}
			_, err = db.ExecContext(ctx, `UPDATE oauth_tokens SET access_token=$1, refresh_token=$2, expires_at=$3, scope=$4, updated_at=NOW() WHERE provider=$5`,
				newAT, newRT, newExp, strings.TrimSpace(newScope), provider)
			if err != nil {
				slog.Warn("token persist failed", slog.String("provider", provider), slog.Any("err", err))
				continue
			}
			slog.Info("token refreshed", slog.String("provider", provider))
		}
	}()
}
