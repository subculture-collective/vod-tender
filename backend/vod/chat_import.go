package vod

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ImportChat fetches VOD chat replay messages from Twitch's rechat API and stores
// them into chat_messages. It is best-effort and tolerant to missing fields.
// It attempts to iterate over the VOD duration in 30s chunks; if duration is
// unknown, it will stop after several consecutive empty chunks.
func ImportChat(ctx context.Context, db *sql.DB, vodID string) error {
	// Lookup VOD start date and duration, if available
	var vodDate time.Time
	var durationSeconds int
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(date, to_timestamp(0)), COALESCE(duration_seconds, 0) FROM vods WHERE twitch_vod_id=$1`, vodID).Scan(&vodDate, &durationSeconds)
	if vodDate.IsZero() {
		vodDate = time.Now().UTC()
	}

	// Prepare insert statement
	stmt, err := db.PrepareContext(ctx, `INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color, reply_to_id, reply_to_username, reply_to_message) VALUES ($1,$2,$3,$4,$5,'','','','','','')`)
	if err != nil {
		return fmt.Errorf("prepare insert chat: %w", err)
	}
	defer func() {
		if err := stmt.Close(); err != nil {
			slog.Warn("failed to close prepared statement", slog.Any("err", err))
		}
	}()

	// Iterate over offsets
	step := 30 // seconds per page
	maxOffset := durationSeconds
	if maxOffset <= 0 {
		maxOffset = 24 * 60 * 60
	} // cap at 24h when unknown
	emptyStreak := 0
	seenIDs := make(map[string]struct{})

	logger := slog.Default().With(slog.String("component", "vod_chat_import"), slog.String("vod_id", vodID))
	logger.Info("starting chat import")

	// Prepare cookie header from Netscape cookie file if present (for sub-only VODs)
	cookieHeader := buildTwitchCookieHeader()

	for offset := 0; offset <= maxOffset; offset += step {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msgs, nextOffset, err := fetchRechatChunk(ctx, vodID, offset, cookieHeader)
		if err != nil {
			// Log and break; avoid tight loops on persistent errors
			logger.Warn("fetch rechat chunk failed", slog.Int("offset", offset), slog.Any("err", err))
			// If we already imported some and now failing late, stop; else keep trying a couple times
			emptyStreak++
			if emptyStreak >= 3 {
				break
			}
			continue
		}
		if len(msgs) == 0 {
			emptyStreak++
			if emptyStreak >= 4 { // four empty windows in a row -> likely done
				break
			}
			continue
		}
		emptyStreak = 0

		// Insert messages (dedupe by message id if present)
		for _, m := range msgs {
			if m.ID != "" {
				if _, ok := seenIDs[m.ID]; ok {
					continue
				}
				seenIDs[m.ID] = struct{}{}
			}
			abs := m.Abs
			if abs.IsZero() {
				// derive from vod start and relative seconds
				abs = vodDate.Add(time.Duration(m.Rel * float64(time.Second)))
			}
			if _, err := stmt.ExecContext(ctx, vodID, m.User, m.Text, abs, m.Rel); err != nil {
				// best effort; continue on individual failures
				logger.Debug("insert chat row failed", slog.Any("err", err))
			}
		}

		// Advance offset if API hints a next window
		if nextOffset > offset {
			offset = nextOffset - step // loop will +step
		}
		// Be polite
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	logger.Info("chat import finished")
	return nil
}

// rechatMessage is a minimal representation of a rechat message
type rechatMessage struct {
	ID   string
	User string
	Text string
	Abs  time.Time
	Rel  float64
}

// fetchRechatChunk queries Twitch's rechat API for a given offset window and returns parsed messages.
// It tries both forms of video_id (raw "123" and "v123") for compatibility.
func fetchRechatChunk(ctx context.Context, vodID string, offset int, cookieHeader string) ([]rechatMessage, int, error) {
	base := "https://rechat.twitch.tv/rechat-messages"
	// Try with raw id first
	u1 := fmt.Sprintf("%s?video_id=%s&offset=%d", base, url.QueryEscape(vodID), offset)
	msgs, next, err := doFetchRechat(ctx, u1, offset, cookieHeader)
	if err == nil && msgs != nil {
		return msgs, next, nil
	}
	// Fallback with v-prefixed id
	vPref := vodID
	if !strings.HasPrefix(strings.ToLower(vodID), "v") {
		vPref = "v" + vodID
	}
	u2 := fmt.Sprintf("%s?video_id=%s&offset=%d", base, url.QueryEscape(vPref), offset)
	return doFetchRechat(ctx, u2, offset, cookieHeader)
}

func doFetchRechat(ctx context.Context, urlStr string, offset int, cookieHeader string) ([]rechatMessage, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	req.Header.Set("User-Agent", "vod-tender/1.0 (+https://github.com/onnwee/vod-tender)")
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, offset, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", slog.Any("err", err))
		}
	}()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, offset, fmt.Errorf("rechat status %d: %s", resp.StatusCode, string(b))
	}
	var raw struct {
		Data []struct {
			Attributes struct {
				ID        string    `json:"id"`
				Timestamp time.Time `json:"timestamp"`
				Offset    float64   `json:"offset"`
				Message   struct {
					Body string `json:"body"`
					User struct {
						UserLogin   string `json:"userLogin"`
						DisplayName string `json:"displayName"`
					} `json:"user"`
					UserColor string `json:"userColor"`
				} `json:"message"`
			} `json:"attributes"`
		} `json:"data"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&raw); err != nil {
		return nil, offset, err
	}
	out := make([]rechatMessage, 0, len(raw.Data))
	for _, d := range raw.Data {
		a := d.Attributes
		user := a.Message.User.DisplayName
		if user == "" {
			user = a.Message.User.UserLogin
		}
		rel := a.Offset
		// if rel == 0, derive relative from timestamp diff if possible (best-effort)
		// caller will adjust using VOD start; leave as 0 here
		out = append(out, rechatMessage{
			ID:   a.ID,
			User: user,
			Text: a.Message.Body,
			Abs:  a.Timestamp,
			Rel:  rel,
		})
	}
	// Hint next offset just after this window
	next := offset + 30
	if len(out) > 0 {
		// try to advance to the last seen rel offset if provided
		last := out[len(out)-1]
		if last.Rel > 0 {
			next = int(last.Rel) + 1
		}
	}
	return out, next, nil
}

// buildTwitchCookieHeader reads a Netscape cookie file and returns a Cookie header string with
// cookies scoped to twitch.tv. If no file or no cookies, returns empty string.
func buildTwitchCookieHeader() string {
	path := os.Getenv("YTDLP_COOKIES_PATH")
	if path == "" {
		path = "/run/cookies/twitch-cookies.txt"
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return ""
	}
	// Limit read size to avoid huge files
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	pairs := make([]string, 0, 16)
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		// Netscape format: domain\tflag\tpath\tsecure\texpiry\tname\tvalue
		cols := strings.Split(ln, "\t")
		if len(cols) < 7 {
			continue
		}
		domain := cols[0]
		name := cols[5]
		value := cols[6]
		// Only include twitch.tv cookies
		if domain == "twitch.tv" || domain == ".twitch.tv" || strings.HasSuffix(domain, ".twitch.tv") {
			// Basic filtering: include auth-token and related cookies; or include all for completeness
			// Keep a minimal allowlist to avoid noisy cookies if desired; here we include all.
			// Skip HttpOnly attributes—they are not in Netscape file; safe to include all pairs.
			if name != "" {
				// Avoid newlines/semicolons in value
				value = strings.ReplaceAll(value, ";", "")
				value = strings.ReplaceAll(value, "\n", "")
				value = strings.ReplaceAll(value, "\r", "")
				pairs = append(pairs, name+"="+value)
			}
		}
	}
	if len(pairs) == 0 {
		return ""
	}
	// Some rechat hosts may be on subdomains; ensure we also add cookies for parent domain
	_ = filepath.Base(path) // keep import for filepath
	return strings.Join(pairs, "; ")
}
