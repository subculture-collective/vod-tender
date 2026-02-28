package chat

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/onnwee/vod-tender/backend/twitchapi"
	vodpkg "github.com/onnwee/vod-tender/backend/vod"
)

// StartAutoChatRecorder polls Twitch stream status and automatically starts the chat recorder
// when the configured channel goes live. It uses a placeholder VOD id (live-<unixStart>) until
// the real VOD is published.
// The channel parameter specifies which Twitch channel to monitor.
// Env knobs:
//
//	CHAT_AUTO_POLL_INTERVAL (default 30s)
//	TWITCH_BOT_USERNAME, TWITCH_CLIENT_ID, TWITCH_CLIENT_SECRET required (plus stored oauth token)
func StartAutoChatRecorder(ctx context.Context, db *sql.DB, channel string) {
	if channel == "" {
		slog.Info("auto chat: TWITCH_CHANNEL empty; abort")
		return
	}
	if os.Getenv("TWITCH_BOT_USERNAME") == "" {
		slog.Info("auto chat: TWITCH_BOT_USERNAME empty; abort")
		return
	}
	clientID := os.Getenv("TWITCH_CLIENT_ID")
	clientSecret := os.Getenv("TWITCH_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		slog.Info("auto chat: missing client id/secret; abort")
		return
	}

	pollEvery := 30 * time.Second
	if v := os.Getenv("CHAT_AUTO_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			pollEvery = d
		}
	}

	var running bool
	ts := &twitchapi.TokenSource{ClientID: clientID, ClientSecret: clientSecret}
	helix := &twitchapi.HelixClient{
		AppTokenSource: ts,
		ClientID:       clientID,
	}
	var startedAt time.Time
	var placeholder string
	var recCancel context.CancelFunc
	reconciled := false

	reconcileDelay := time.Minute
	if v := os.Getenv("VOD_RECONCILE_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			reconcileDelay = d
		}
	}
	reconcileWindow := 15 * time.Minute // how long after offline we keep trying

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	slog.Info("auto chat: started poller", slog.Duration("interval", pollEvery))
	for {
		if ctx.Err() != nil {
			return
		}
		func() {
			// If we're running, check if stream still live; if not, stop recorder and reconcile.
			// (no-op check removed - logic continues to fetch live status)
			streams, err := helix.GetStreams(ctx, channel)
			if err != nil {
				slog.Debug("auto chat: streams req", slog.Any("err", err))
				return
			}
			if len(streams) == 0 {
				// Offline
				if running && !reconciled {
					// Stop current recorder if not already stopped
					if recCancel != nil {
						recCancel()
					}
					offStarted := time.Now()
					slog.Info("auto chat: stream ended; beginning reconciliation window", slog.String("placeholder_vod", placeholder))
					go func(ph string, st time.Time, offAt time.Time) {
						// Wait initial delay
						select {
						case <-ctx.Done():
							return
						case <-time.After(reconcileDelay):
						}
						for attempt := 0; attempt < 30; attempt++ { // roughly up to reconcileWindow depending on delay
							if ctx.Err() != nil {
								return
							}
							if time.Since(offAt) > reconcileWindow {
								slog.Warn("auto chat: reconciliation window expired", slog.String("placeholder_vod", ph))
								return
							}
							// Fetch channel VODs and find best match
							vods, err := vodpkg.FetchChannelVODs(ctx, channel)
							if err == nil {
								var candidate *vodpkg.VOD
								for i := range vods {
									v := vods[i]
									if v.Date.Before(st.Add(-10*time.Minute)) || v.Date.After(st.Add(10*time.Minute)) {
										continue
									}
									if candidate == nil {
										candidate = &v
									} else if v.Date.After(candidate.Date) && !v.Date.Before(st) {
										candidate = &v
									}
								}
								if candidate != nil {
									tx, err := db.BeginTx(ctx, nil)
									if err != nil {
										slog.Warn("auto chat: reconcile begin tx", slog.Any("err", err))
										return
									}
									// Ensure real VOD row; then refresh metadata (title/date/duration)
									_, _ = tx.ExecContext(ctx, `INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) VALUES ($1,$2,$3,$4,$5,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, channel, candidate.ID, candidate.Title, candidate.Date, candidate.Duration)
									_, _ = tx.ExecContext(ctx, `UPDATE vods SET title=$1, date=$2, duration_seconds=$3, updated_at=NOW() WHERE channel=$4 AND twitch_vod_id=$5`, candidate.Title, candidate.Date, candidate.Duration, channel, candidate.ID)
									// Realign relative timestamps if placeholder start differs from actual VOD date
									if !candidate.Date.Equal(st) {
										delta := candidate.Date.Sub(st).Seconds()
										// Shift existing rel_timestamp by delta (if candidate.Date later, delta positive)
										if _, err := tx.ExecContext(ctx, `UPDATE chat_messages SET rel_timestamp=rel_timestamp - $1 WHERE channel=$2 AND vod_id=$3`, delta, channel, ph); err != nil {
											_ = tx.Rollback()
											slog.Warn("auto chat: reconcile shift timestamps", slog.Any("err", err))
											return
										}
									}
									if _, err := tx.ExecContext(ctx, `UPDATE chat_messages SET vod_id=$1 WHERE channel=$2 AND vod_id=$3`, candidate.ID, channel, ph); err != nil {
										_ = tx.Rollback()
										slog.Warn("auto chat: reconcile update chat", slog.Any("err", err))
										return
									}
									if _, err := tx.ExecContext(ctx, `DELETE FROM vods WHERE channel=$1 AND twitch_vod_id=$2`, channel, ph); err != nil {
										_ = tx.Rollback()
										slog.Warn("auto chat: reconcile delete placeholder", slog.Any("err", err))
										return
									}
									if err := tx.Commit(); err != nil {
										slog.Warn("auto chat: reconcile commit", slog.Any("err", err))
										return
									}
									slog.Info("auto chat: reconciliation complete", slog.String("placeholder", ph), slog.String("real_vod", candidate.ID), slog.String("channel", channel))
									reconciled = true
									running = false
									return
								}
							} else {
								slog.Debug("auto chat: reconcile fetch vods", slog.Any("err", err))
							}
							select {
							case <-ctx.Done():
								return
							case <-time.After(30 * time.Second):
							}
						}
					}(placeholder, startedAt, offStarted)
				}
				return
			}
			// Stream is live
			if running {
				return
			} // already recording
			startedAt = streams[0].StartedAt.UTC()
			placeholder = fmt.Sprintf("live-%d", startedAt.Unix())
			reconciled = false
			_, _ = db.ExecContext(ctx, `INSERT INTO vods (channel, twitch_vod_id, title, date, duration_seconds, created_at) VALUES ($1,$2,$3,$4,$5,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, channel, placeholder, "LIVE: "+streams[0].Title, startedAt, 0)
			running = true
			slog.Info("auto chat: stream live; starting chat recorder", slog.String("vod_id", placeholder), slog.Time("started_at", startedAt), slog.String("channel", channel))
			recCtx, cancel := context.WithCancel(ctx)
			recCancel = cancel
			go func(pID string, st time.Time) {
				StartTwitchChatRecorder(recCtx, db, pID, st)
				slog.Info("auto chat: recorder goroutine exited", slog.String("vod_id", pID))
			}(placeholder, startedAt)
		}()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
