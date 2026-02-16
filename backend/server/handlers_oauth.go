package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/onnwee/vod-tender/backend/config"
	dbpkg "github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/twitchapi"
	"github.com/onnwee/vod-tender/backend/youtubeapi"
)

// oauthTokenStore adapts the DB to youtubeapi.TokenStore interface
type oauthTokenStore struct{ db *sql.DB }

func (o *oauthTokenStore) UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error {
	// Use dbpkg.UpsertOAuthToken which handles encryption automatically
	return dbpkg.UpsertOAuthToken(ctx, o.db, provider, accessToken, refreshToken, expiry, raw, "")
}
func (o *oauthTokenStore) GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error) {
	// Use dbpkg.GetOAuthToken which handles decryption automatically
	access, refresh, exp, scope, dbErr := dbpkg.GetOAuthToken(ctx, o.db, provider)
	return access, refresh, exp, scope, dbErr
}


// HandleTwitchOAuthStart initiates the Twitch OAuth flow by redirecting to Twitch.
func (h *Handlers) HandleTwitchOAuthStart(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load() // ignore error; minimal usage
	if cfg.TwitchClientID == "" || cfg.TwitchRedirectURI == "" {
		http.Error(w, "oauth not configured (need TWITCH_CLIENT_ID + TWITCH_REDIRECT_URI)", http.StatusBadRequest)
		return
	}
	// generate state
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "state gen error", 500)
		return
	}
	st := hex.EncodeToString(b)
	h.addOAuthState(st, time.Now().Add(10*time.Minute))
	authURL, err := twitchapi.BuildAuthorizeURL(cfg.TwitchClientID, cfg.TwitchRedirectURI, cfg.TwitchScopes, st)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleTwitchOAuthCallback handles the OAuth callback from Twitch and stores tokens.
func (h *Handlers) HandleTwitchOAuthCallback(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	code := r.URL.Query().Get("code")
	st := r.URL.Query().Get("state")
	if code == "" || st == "" {
		http.Error(w, "missing code/state", 400)
		return
	}
	// validate state
	h.stateMu.RLock()
	exp, ok := h.stateStore[st]
	h.stateMu.RUnlock()
	if !ok || time.Now().After(exp) {
		http.Error(w, "invalid state", 400)
		return
	}
	h.stateMu.Lock()
	delete(h.stateStore, st)
	h.stateMu.Unlock()
	ctx := r.Context()
	res, err := twitchapi.ExchangeAuthCode(ctx, cfg.TwitchClientID, cfg.TwitchClientSecret, code, cfg.TwitchRedirectURI)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// persist tokens using dbpkg.UpsertOAuthToken (handles encryption)
	dbErr := dbpkg.UpsertOAuthToken(ctx, h.db, "twitch", res.AccessToken, res.RefreshToken,
		twitchapi.ComputeExpiry(res.ExpiresIn), "", strings.Join(res.Scope, " "))
	if dbErr != nil {
		http.Error(w, dbErr.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "scopes": res.Scope, "expires_in": res.ExpiresIn}); err != nil {
		slog.Warn("failed to encode JSON response", slog.Any("err", err))
	}
}

// HandleYouTubeOAuthStart initiates the YouTube OAuth flow.
func (h *Handlers) HandleYouTubeOAuthStart(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	if cfg.YTClientID == "" || cfg.YTRedirectURI == "" {
		http.Error(w, "youtube oauth not configured", 400)
		return
	}
	// generate state
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "state gen error", 500)
		return
	}
	st := hex.EncodeToString(b)
	h.addOAuthState(st, time.Now().Add(10*time.Minute))
	// Build auth URL manually (reuse youtubeapi oauth config)
	ts := &oauthTokenStore{db: h.db}
	yts := youtubeapi.New(cfg, ts)
	authURL := yts.AuthCodeURL(st)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleYouTubeOAuthCallback handles the OAuth callback from YouTube and stores tokens.
func (h *Handlers) HandleYouTubeOAuthCallback(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	code := r.URL.Query().Get("code")
	st := r.URL.Query().Get("state")
	if code == "" || st == "" {
		http.Error(w, "missing code/state", 400)
		return
	}
	h.stateMu.RLock()
	exp, ok := h.stateStore[st]
	h.stateMu.RUnlock()
	if !ok || time.Now().After(exp) {
		http.Error(w, "invalid state", 400)
		return
	}
	h.stateMu.Lock()
	delete(h.stateStore, st)
	h.stateMu.Unlock()
	ts := &oauthTokenStore{db: h.db}
	yts := youtubeapi.New(cfg, ts)
	tok, err := yts.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "expiry": tok.Expiry, "access_token_present": tok.AccessToken != "", "refresh_token_present": tok.RefreshToken != ""}); err != nil {
		slog.Warn("failed to encode JSON response", slog.Any("err", err))
	}
}
