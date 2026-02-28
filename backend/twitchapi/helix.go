// Package twitchapi contains minimal helpers to interact with Twitch Helix APIs
// for user id resolution and listing archived VODs, using an app access token.
package twitchapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	helixBaseURL    = "https://api.twitch.tv"
	helixMaxRetries = 4
)

// HelixClient provides minimal methods needed for VOD discovery.
type HelixClient struct {
	AppTokenSource *TokenSource
	HTTPClient     *http.Client
	ClientID       string
}

func (hc *HelixClient) http() *http.Client {
	if hc.HTTPClient != nil {
		return hc.HTTPClient
	}
	return http.DefaultClient
}

func (hc *HelixClient) requestJSON(ctx context.Context, path string, query url.Values, out any) error {
	if hc.AppTokenSource == nil {
		return fmt.Errorf("missing app token source")
	}
	if hc.ClientID == "" {
		return fmt.Errorf("missing twitch client id")
	}

	refreshedAfter401 := false
	for attempt := 1; attempt <= helixMaxRetries; attempt++ {
		tok, err := hc.AppTokenSource.Get(ctx)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, helixBaseURL+path, nil)
		if err != nil {
			return err
		}
		req.URL.RawQuery = query.Encode()
		req.Header.Set("Client-Id", hc.ClientID)
		req.Header.Set("Authorization", "Bearer "+tok)

		resp, err := hc.http().Do(req)
		if err != nil {
			if attempt == helixMaxRetries {
				return err
			}
			if err := sleepWithContext(ctx, helixBackoff(attempt)); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode == http.StatusUnauthorized && !refreshedAfter401 {
			_ = resp.Body.Close()
			// Force refresh and retry once.
			hc.AppTokenSource.SetToken("", time.Time{})
			if _, err := hc.AppTokenSource.Get(ctx); err != nil {
				return fmt.Errorf("refresh app token after 401: %w", err)
			}
			refreshedAfter401 = true
			// If the 401 occurred on the final attempt, roll back the counter so
			// the refreshed token still gets at least one more request.
			if attempt == helixMaxRetries {
				attempt--
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < helixMaxRetries {
			delay := helixRateLimitDelay(resp.Header)
			slog.Warn("twitch helix rate limited; retrying",
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
				slog.String("remaining", resp.Header.Get("Ratelimit-Remaining")),
				slog.String("reset", resp.Header.Get("Ratelimit-Reset")),
			)
			_ = resp.Body.Close()
			if err := sleepWithContext(ctx, delay); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode >= 500 && resp.StatusCode <= 599 && attempt < helixMaxRetries {
			delay := helixBackoff(attempt)
			slog.Warn("twitch helix server error; retrying",
				slog.Int("status", resp.StatusCode),
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
			)
			_ = resp.Body.Close()
			if err := sleepWithContext(ctx, delay); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			msg := strings.TrimSpace(string(b))
			if msg == "" {
				msg = http.StatusText(resp.StatusCode)
			}
			return fmt.Errorf("helix %s failed: %s (%s)", path, resp.Status, msg)
		}

		err = json.NewDecoder(resp.Body).Decode(out)
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Warn("failed to close response body", slog.Any("err", closeErr))
		}
		if err != nil {
			return fmt.Errorf("decode helix %s response: %w", path, err)
		}
		return nil
	}

	return fmt.Errorf("helix %s failed after retries", path)
}

func helixBackoff(attempt int) time.Duration {
	d := 250 * time.Millisecond * time.Duration(1<<(attempt-1))
	if d > 2*time.Second {
		return 2 * time.Second
	}
	return d
}

func helixRateLimitDelay(header http.Header) time.Duration {
	if v := strings.TrimSpace(header.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			if secs < 0 {
				secs = 0
			}
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil {
			d := time.Until(t)
			if d > 0 {
				if d > 30*time.Second {
					return 30 * time.Second
				}
				return d
			}
		}
	}

	if reset := strings.TrimSpace(header.Get("Ratelimit-Reset")); reset != "" {
		if unix, err := strconv.ParseInt(reset, 10, 64); err == nil {
			d := time.Until(time.Unix(unix, 0))
			if d > 0 {
				if d > 30*time.Second {
					return 30 * time.Second
				}
				return d
			}
		}
	}

	return 1 * time.Second
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// GetUserID resolves a login name to its user ID.
func (hc *HelixClient) GetUserID(ctx context.Context, login string) (string, error) {
	if login == "" {
		return "", fmt.Errorf("login empty")
	}
	q := url.Values{}
	q.Set("login", login)
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := hc.requestJSON(ctx, "/helix/users", q, &body); err != nil {
		return "", err
	}
	if len(body.Data) == 0 {
		return "", fmt.Errorf("user not found")
	}
	return body.Data[0].ID, nil
}

// ListVideos lists archive videos for a user.
type VideoMeta struct{ ID, Title, Duration, CreatedAt string }

func (hc *HelixClient) ListVideos(ctx context.Context, userID, after string, first int) ([]VideoMeta, string, error) {
	if userID == "" {
		return nil, "", fmt.Errorf("userID empty")
	}
	if first <= 0 {
		first = 20
	}
	q := url.Values{}
	q.Set("user_id", userID)
	q.Set("type", "archive")
	q.Set("first", fmt.Sprintf("%d", first))
	if after != "" {
		q.Set("after", after)
	}
	var body struct {
		Pagination struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
		Data []struct {
			ID, Title, Duration string
			CreatedAt           string `json:"created_at"`
		} `json:"data"`
	}
	if err := hc.requestJSON(ctx, "/helix/videos", q, &body); err != nil {
		return nil, "", err
	}
	out := make([]VideoMeta, 0, len(body.Data))
	for _, v := range body.Data {
		out = append(out, VideoMeta{ID: v.ID, Title: v.Title, Duration: v.Duration, CreatedAt: v.CreatedAt})
	}
	return out, body.Pagination.Cursor, nil
}

// StreamMeta represents a minimal Helix stream payload for live status checks.
type StreamMeta struct {
	StartedAt time.Time `json:"started_at"`
	Title     string    `json:"title"`
}

// GetStreams fetches current live stream information for a channel login.
func (hc *HelixClient) GetStreams(ctx context.Context, userLogin string) ([]StreamMeta, error) {
	if strings.TrimSpace(userLogin) == "" {
		return nil, fmt.Errorf("userLogin empty")
	}
	q := url.Values{}
	q.Set("user_login", userLogin)

	var body struct {
		Data []StreamMeta `json:"data"`
	}
	if err := hc.requestJSON(ctx, "/helix/streams", q, &body); err != nil {
		return nil, err
	}
	return body.Data, nil
}
