package twitchapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenSource fetches and caches a Twitch app access (client credentials) token.
// NOTE: This token CANNOT be used for IRC chat; chat requires a user (bot) OAuth token with chat:read/chat:edit scopes.
type TokenSource struct {
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client

	mu        sync.RWMutex
	token     string
	expiresAt time.Time
}

// Get returns a valid (fresh or cached) app access token.
func (ts *TokenSource) Get(ctx context.Context) (string, error) {
	ts.mu.RLock()
	if ts.token != "" && time.Until(ts.expiresAt) > 60*time.Second { // 1 min buffer
		tok := ts.token
		ts.mu.RUnlock()
		return tok, nil
	}
	ts.mu.RUnlock()
	return ts.refresh(ctx)
}

func (ts *TokenSource) refresh(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.token != "" && time.Until(ts.expiresAt) > 60*time.Second {
		return ts.token, nil
	}
	if ts.ClientID == "" || ts.ClientSecret == "" {
		return "", errors.New("missing client id/secret for twitch app token")
	}
	form := url.Values{}
	form.Set("client_id", ts.ClientID)
	form.Set("client_secret", ts.ClientSecret)
	form.Set("grant_type", "client_credentials")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := ts.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", slog.Any("err", err))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("twitch token request failed: %s: %s", resp.Status, string(b))
	}
	var at struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&at); err != nil {
		return "", err
	}
	if at.AccessToken == "" {
		return "", errors.New("empty access_token in twitch response")
	}
	ts.token = at.AccessToken
	ts.expiresAt = time.Now().Add(time.Duration(at.ExpiresIn) * time.Second)
	return ts.token, nil
}
