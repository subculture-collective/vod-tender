// Package youtubeapi wraps Google OAuth2 client config and the YouTube Data API
// for the single purpose of uploading VOD videos. Tokens are persisted via the
// provided TokenStore interface so they can be refreshed and reused by workers.
package youtubeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	yt "google.golang.org/api/youtube/v3"

	"github.com/onnwee/vod-tender/backend/config"
)

const provider = "youtube"

type TokenStore interface {
    UpsertOAuthToken(ctx context.Context, provider string, accessToken string, refreshToken string, expiry time.Time, raw string) error
    GetOAuthToken(ctx context.Context, provider string) (accessToken string, refreshToken string, expiry time.Time, raw string, err error)
}

type Service struct {
    cfg    *config.Config
    db     TokenStore
    oauth  *oauth2.Config
}

func New(cfg *config.Config, ts TokenStore) *Service {
    scopes := []string{"https://www.googleapis.com/auth/youtube.upload"}
    if cfg.YTScopes != "" {
        // allow comma or space separated
        s := strings.ReplaceAll(cfg.YTScopes, ",", " ")
        fields := strings.Fields(s)
        if len(fields) > 0 { scopes = fields }
    }
    oauth := &oauth2.Config{
        ClientID:     cfg.YTClientID,
        ClientSecret: cfg.YTClientSecret,
        Endpoint:     google.Endpoint,
        RedirectURL:  cfg.YTRedirectURI,
        Scopes:       scopes,
    }
    return &Service{cfg: cfg, db: ts, oauth: oauth}
}

func (s *Service) AuthCodeURL(state string) string {
    return s.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

func (s *Service) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
    tok, err := s.oauth.Exchange(ctx, code)
    if err != nil { return nil, err }
    rawBytes, _ := json.Marshal(tok)
    _ = s.db.UpsertOAuthToken(ctx, provider, tok.AccessToken, tok.RefreshToken, tok.Expiry, string(rawBytes))
    return tok, nil
}

func (s *Service) refreshIfNeeded(ctx context.Context) (*oauth2.Token, error) {
    access, refresh, expiry, raw, err := s.db.GetOAuthToken(ctx, provider)
    if err != nil { return nil, err }
    if access == "" {
        return nil, errors.New("no youtube token stored")
    }
    var tok oauth2.Token
    if raw != "" { _ = json.Unmarshal([]byte(raw), &tok) }
    if tok.AccessToken == "" { tok.AccessToken = access }
    tok.RefreshToken = refresh
    tok.Expiry = expiry
    if time.Until(tok.Expiry) > 2*time.Minute { return &tok, nil }
    ts := s.oauth.TokenSource(ctx, &tok)
    newTok, err := ts.Token()
    if err != nil { return &tok, err }
    rawBytes, _ := json.Marshal(newTok)
    _ = s.db.UpsertOAuthToken(ctx, provider, newTok.AccessToken, newTok.RefreshToken, newTok.Expiry, string(rawBytes))
    return newTok, nil
}

func (s *Service) Client(ctx context.Context) (*yt.Service, error) {
    tok, err := s.refreshIfNeeded(ctx)
    if err != nil { return nil, err }
    client := s.oauth.Client(ctx, tok)
    return yt.New(client)
}

// UploadVideo uploads a video file at path with given title/description/privacy using provided YouTube service.
func UploadVideo(ctx context.Context, svc *yt.Service, path, title, description, privacy string) (string, error) {
    if svc == nil { return "", fmt.Errorf("nil youtube service") }
    if privacy == "" { privacy = "private" }
    f, err := os.Open(path)
    if err != nil { return "", fmt.Errorf("open file: %w", err) }
    defer f.Close()
    snippet := &yt.VideoSnippet{Title: title, Description: description}
    status := &yt.VideoStatus{PrivacyStatus: privacy}
    video := &yt.Video{Snippet: snippet, Status: status}
    call := svc.Videos.Insert([]string{"snippet","status"}, video).Media(f)
    res, err := call.Do()
    if err != nil { return "", fmt.Errorf("youtube upload: %w", err) }
    if res.Id == "" { return "", fmt.Errorf("youtube upload: empty id") }
    return "https://www.youtube.com/watch?v=" + res.Id, nil
}
