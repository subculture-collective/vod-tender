// Package twitchapi contains minimal helpers to interact with Twitch Helix APIs
// for user id resolution and listing archived VODs, using an app access token.
package twitchapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// HelixClient provides minimal methods needed for VOD discovery.
type HelixClient struct {
	AppTokenSource *TokenSource
	ClientID       string
	HTTPClient     *http.Client
}

func (hc *HelixClient) http() *http.Client {
	if hc.HTTPClient != nil {
		return hc.HTTPClient
	}
	return http.DefaultClient
}

// GetUserID resolves a login name to its user ID.
func (hc *HelixClient) GetUserID(ctx context.Context, login string) (string, error) {
	if login == "" {
		return "", fmt.Errorf("login empty")
	}
	tok, err := hc.AppTokenSource.Get(ctx)
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/users", nil)
	q := req.URL.Query()
	q.Set("login", login)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Client-Id", hc.ClientID)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := hc.http().Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", slog.Any("err", err))
		}
	}()
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
	tok, err := hc.AppTokenSource.Get(ctx)
	if err != nil {
		return nil, "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/videos", nil)
	q := req.URL.Query()
	q.Set("user_id", userID)
	q.Set("type", "archive")
	q.Set("first", fmt.Sprintf("%d", first))
	if after != "" {
		q.Set("after", after)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Client-Id", hc.ClientID)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := hc.http().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", slog.Any("err", err))
		}
	}()
	var body struct {
		Data []struct {
			ID, Title, Duration string
			CreatedAt           string `json:"created_at"`
		} `json:"data"`
		Pagination struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", err
	}
	out := make([]VideoMeta, 0, len(body.Data))
	for _, v := range body.Data {
		out = append(out, VideoMeta{ID: v.ID, Title: v.Title, Duration: v.Duration, CreatedAt: v.CreatedAt})
	}
	return out, body.Pagination.Cursor, nil
}
