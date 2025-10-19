package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/onnwee/vod-tender/backend/twitchapi"
	vodpkg "github.com/onnwee/vod-tender/backend/vod"
)

// StartAutoChatRecorder polls Twitch stream status and automatically starts the chat recorder
// when the configured channel goes live. It uses a placeholder VOD id (live-<unixStart>) until
// the real VOD is published.
// Env knobs:
//
//	CHAT_AUTO_POLL_INTERVAL (default 30s)
//	TWITCH_CHANNEL, TWITCH_BOT_USERNAME, TWITCH_CLIENT_ID, TWITCH_CLIENT_SECRET required (plus stored oauth token)
func StartAutoChatRecorder(ctx context.Context, db *sql.DB) {
	channel := os.Getenv("TWITCH_CHANNEL")
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

	getAppToken := func(ctx context.Context) (string, error) { return ts.Get(ctx) }

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	slog.Info("auto chat: started poller", slog.Duration("interval", pollEvery))
	for {
		if ctx.Err() != nil {
			return
		}
		func() {
			// If we're running, check if stream still live; if not, stop recorder and reconcile.
			if running {
				// We'll re-check live status below; logic continues after fetch.
			}
			tok, err := getAppToken(ctx)
			if err != nil {
				slog.Debug("auto chat: get app token", slog.Any("err", err))
				return
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/streams", nil)
			q := req.URL.Query()
			q.Set("user_login", channel)
			req.URL.RawQuery = q.Encode()
			req.Header.Set("Client-Id", clientID)
			req.Header.Set("Authorization", "Bearer "+tok)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				slog.Debug("auto chat: streams req", slog.Any("err", err))
				return
			}
			defer resp.Body.Close()
			var body struct {
				Data []struct {
					Title     string    `json:"title"`
					StartedAt time.Time `json:"started_at"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				slog.Debug("auto chat: decode", slog.Any("err", err))
				return
			}
			if len(body.Data) == 0 {
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
							vods, err := vodpkg.FetchChannelVODs(ctx)
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
									tx, err := db.Begin()
									if err != nil {
										slog.Warn("auto chat: reconcile begin tx", slog.Any("err", err))
										return
									}
									// Ensure real VOD row; then refresh metadata (title/date/duration)
									_, _ = tx.Exec(`INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at) VALUES ($1,$2,$3,$4,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, candidate.ID, candidate.Title, candidate.Date, candidate.Duration)
									_, _ = tx.Exec(`UPDATE vods SET title=$1, date=$2, duration_seconds=$3, updated_at=NOW() WHERE twitch_vod_id=$4`, candidate.Title, candidate.Date, candidate.Duration, candidate.ID)
									// Realign relative timestamps if placeholder start differs from actual VOD date
									if !candidate.Date.Equal(st) {
										delta := candidate.Date.Sub(st).Seconds()
										// Shift existing rel_timestamp by delta (if candidate.Date later, delta positive)
										if _, err := tx.Exec(`UPDATE chat_messages SET rel_timestamp=rel_timestamp - $1 WHERE vod_id=$2`, delta, ph); err != nil {
											_ = tx.Rollback()
											slog.Warn("auto chat: reconcile shift timestamps", slog.Any("err", err))
											return
										}
									}
									if _, err := tx.Exec(`UPDATE chat_messages SET vod_id=$1 WHERE vod_id=$2`, candidate.ID, ph); err != nil {
										_ = tx.Rollback()
										slog.Warn("auto chat: reconcile update chat", slog.Any("err", err))
										return
									}
									if _, err := tx.Exec(`DELETE FROM vods WHERE twitch_vod_id=$1`, ph); err != nil {
										_ = tx.Rollback()
										slog.Warn("auto chat: reconcile delete placeholder", slog.Any("err", err))
										return
									}
									if err := tx.Commit(); err != nil {
										slog.Warn("auto chat: reconcile commit", slog.Any("err", err))
										return
									}
									slog.Info("auto chat: reconciliation complete", slog.String("placeholder", ph), slog.String("real_vod", candidate.ID))
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
			startedAt = body.Data[0].StartedAt.UTC()
			placeholder = fmt.Sprintf("live-%d", startedAt.Unix())
			reconciled = false
			_, _ = db.Exec(`INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at) VALUES ($1,$2,$3,$4,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, placeholder, "LIVE: "+body.Data[0].Title, startedAt, 0)
			running = true
			slog.Info("auto chat: stream live; starting chat recorder", slog.String("vod_id", placeholder), slog.Time("started_at", startedAt))
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
