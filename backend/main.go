package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/joho/godotenv"
	"github.com/onnwee/vod-tender/backend/chat"
	"github.com/onnwee/vod-tender/backend/config"
	"github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/oauth"
	"github.com/onnwee/vod-tender/backend/server"
	"github.com/onnwee/vod-tender/backend/telemetry"
	"github.com/onnwee/vod-tender/backend/twitchapi"
	"github.com/onnwee/vod-tender/backend/vod"
)

func main() {
	// Load .env file if present
	_ = godotenv.Load("backend/.env")

	// Configure logging (level + format). Defaults: level=info, format=text.
	lvl := slog.LevelInfo
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug": lvl = slog.LevelDebug
	case "warn": lvl = slog.LevelWarn
	case "error": lvl = slog.LevelError
	case "info", "":
		// keep default
	default:
		// unknown level -> keep info but note once using temporary logger
		tmp := slog.New(slog.NewTextHandler(os.Stdout, nil))
		tmp.Warn("unknown LOG_LEVEL, using info", slog.String("value", os.Getenv("LOG_LEVEL")))
	}
	format := strings.ToLower(os.Getenv("LOG_FORMAT")) // text | json
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	default:
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	}
	slog.SetDefault(slog.New(handler))
	slog.Info("logger initialized", slog.String("level", lvl.String()), slog.String("format", map[bool]string{true: "json", false: "text"}[format=="json"]))

	// Config
	cfg, err := config.Load()
	// Metrics / telemetry init
	telemetry.Init()
	if err != nil {
		slog.Error("config load failed", slog.Any("err", err))
		os.Exit(1)
	}

	// Fetch an app access token (NOT for chat) if client id/secret provided.
	if cfg.TwitchClientID != "" && cfg.TwitchClientSecret != "" {
		ctx2, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		if tok, err := (&twitchapi.TokenSource{ClientID: cfg.TwitchClientID, ClientSecret: cfg.TwitchClientSecret}).Get(ctx2); err != nil {
			slog.Warn("twitch app token fetch failed", slog.Any("err", err))
		} else if len(tok) > 6 {
			masked := "***" + tok[len(tok)-6:]
			slog.Info("twitch app token acquired", slog.String("tail", masked))
		}
		cancel()
	}

	// DB
	database, err := db.Connect()
	if err != nil {
		slog.Error("failed to open db", slog.Any("err", err))
		os.Exit(1)
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		slog.Error("failed to migrate db", slog.Any("err", err))
		os.Exit(1)
	}

	// Root context with graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start services
	// Chat recorder: direct start or auto mode
	if os.Getenv("CHAT_AUTO_START") == "1" {
		go chat.StartAutoChatRecorder(ctx, database)
	} else if err := cfg.ValidateChatReady(); err == nil {
		go chat.StartTwitchChatRecorder(ctx, database, cfg.TwitchVODID, cfg.TwitchVODStart)
	} else {
		slog.Info("chat recorder disabled (missing twitch creds or auto not enabled)")
	}
	go vod.StartVODProcessingJob(ctx, database)
	go vod.StartVODCatalogBackfillJob(ctx, database)

	// Centralized OAuth token refreshers
	oauth.StartRefresher(ctx, database, "twitch", 5*time.Minute, 15*time.Minute, func(rctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		res, err := twitchapi.RefreshToken(rctx, cfg.TwitchClientID, cfg.TwitchClientSecret, refreshToken)
		if err != nil { return "", "", time.Time{}, "", err }
		return res.AccessToken, res.RefreshToken, twitchapi.ComputeExpiry(res.ExpiresIn), strings.Join(res.Scope, " "), nil
	})
	oauth.StartRefresher(ctx, database, "youtube", 10*time.Minute, 20*time.Minute, func(rctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		cfg2, _ := config.Load()
		ts := &oauth2.Token{RefreshToken: refreshToken}
		if cfg2.YTClientID == "" { return "", "", time.Time{}, "", context.Canceled }
		oc := &oauth2.Config{ClientID: cfg2.YTClientID, ClientSecret: cfg2.YTClientSecret, Endpoint: google.Endpoint, RedirectURL: cfg2.YTRedirectURI}
		newTok, err := oc.TokenSource(rctx, ts).Token()
		if err != nil { return "", "", time.Time{}, "", err }
		return newTok.AccessToken, newTok.RefreshToken, newTok.Expiry, "", nil
	})

	// HTTP server (health endpoint)
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	go func() {
		if err := server.Start(ctx, database, addr); err != nil {
			slog.Error("http server exited with error", slog.Any("err", err))
		}
	}()

	// Block until shutdown signal
	<-ctx.Done()
	slog.Info("shutting down")
}

