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
	"time"
)

type AuthCodeExchangeResult struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	TokenType    string   `json:"token_type"`
	Scope        []string `json:"scope"`
	ExpiresIn    int      `json:"expires_in"`
}

// RefreshResult represents the response from a refresh_token grant.
type RefreshResult struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	TokenType    string   `json:"token_type"`
	Scope        []string `json:"scope"`
	ExpiresIn    int      `json:"expires_in"`
}

// BuildAuthorizeURL constructs the user authorization URL for OAuth code grant.
func BuildAuthorizeURL(clientID, redirectURI, scopes, state string) (string, error) {
	if clientID == "" || redirectURI == "" {
		return "", errors.New("missing clientID or redirectURI")
	}
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", clientID)
	v.Set("redirect_uri", redirectURI)
	if scopes != "" {
		v.Set("scope", strings.TrimSpace(strings.ReplaceAll(scopes, ",", " ")))
	}
	if state != "" {
		v.Set("state", state)
	}
	return "https://id.twitch.tv/oauth2/authorize?" + v.Encode(), nil
}

// ExchangeAuthCode exchanges an authorization code for access & refresh tokens.
func ExchangeAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*AuthCodeExchangeResult, error) {
	if clientID == "" || clientSecret == "" || code == "" || redirectURI == "" {
		return nil, errors.New("missing required parameter for auth code exchange")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", slog.Any("err", err))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("twitch auth code exchange failed: %s: %s", resp.Status, string(b))
	}
	var res AuthCodeExchangeResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ComputeExpiry returns absolute expiry time from seconds, defaulting to +60m when unknown.
func ComputeExpiry(seconds int) time.Time {
	if seconds <= 0 {
		return time.Now().Add(60 * time.Minute)
	}
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

// RefreshToken exchanges a refresh token for a new access token.
func RefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*RefreshResult, error) {
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		return nil, errors.New("missing clientID/clientSecret/refreshToken")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", slog.Any("err", err))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("twitch refresh failed: %s: %s", resp.Status, string(b))
	}
	var res RefreshResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}
