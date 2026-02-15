// Command backend is the main entrypoint for the vod-tender API and background workers.
// It:
//   - Loads configuration and initializes structured logging.
//   - Connects to Postgres and runs idempotent migrations.
//   - Starts background jobs: chat recorder (manual or auto), VOD processing,
//     VOD catalog backfill, and OAuth token refreshers for Twitch/YouTube.
//   - Exposes a minimal HTTP server with /healthz, /status, and /metrics.
//
// Shutdown is graceful on SIGINT/SIGTERM.
package main

import (
	"context"
	"log/slog"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // G108: pprof endpoints enabled only when ENABLE_PPROF=1
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
	// Load .env file if present (local dev convenience only; production relies on real env)
	_ = godotenv.Load("backend/.env")

	// Configure logging (level + format). Defaults: level=info, format=text.
	lvl := slog.LevelInfo
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
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
	slog.Info("logger initialized", slog.String("level", lvl.String()), slog.String("format", map[bool]string{true: "json", false: "text"}[format == "json"]))

	// Config
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", slog.Any("err", err))
		os.Exit(1)
	}
	
	// Metrics / telemetry init
	telemetry.Init()
	
	// Initialize OpenTelemetry tracing (optional; requires OTEL_EXPORTER_OTLP_ENDPOINT)
	shutdown, err := telemetry.InitTracing("vod-tender", "1.0.0")
	if err != nil {
		slog.Error("tracing initialization failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer shutdown()

	// Best-effort: fetch a Twitch app access token (client-credentials) if client id/secret provided.
	// This token is used for Helix API calls (discovery, auto-chat polling). It is NOT used for IRC chat.
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
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("failed to close database", slog.Any("err", err))
		}
	}()
	
	// Run database migrations using dual-system approach:
	// 1. Primary: versioned migrations (golang-migrate) from db/migrations/
	// 2. Fallback: embedded SQL (db.Migrate) for backward compatibility
	//
	// New deployments use versioned migrations with proper version tracking.
	// Old deployments without schema_migrations table fall back to embedded SQL.
	// This ensures smooth transition and zero-downtime upgrades.
	//
	// See docs/MIGRATIONS.md for detailed migration architecture.
	slog.Info("running database migrations", slog.String("component", "db_migrate"))
	if err := db.RunMigrations(database); err != nil {
		slog.Warn("versioned migrations failed, attempting fallback to legacy embedded SQL",
			slog.Any("err", err),
			slog.String("component", "db_migrate"))
		// Fallback to embedded SQL migration for backward compatibility with pre-migration deployments
		migrationCtx := context.Background()
		if err := db.Migrate(migrationCtx, database); err != nil {
			slog.Error("failed to migrate db (both versioned and embedded SQL failed)", slog.Any("err", err))
			os.Exit(1)
		}
		slog.Info("legacy embedded SQL migration completed successfully (consider migrating to versioned migrations)",
			slog.String("component", "db_migrate"))
	} else {
		slog.Info("versioned migrations completed successfully",
			slog.String("component", "db_migrate"))
	}

	// Root context with graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Multi-channel support: start workers for each configured channel
	// Falls back to single-channel mode if TwitchChannels is empty
	channels := cfg.TwitchChannels
	if len(channels) == 0 {
		// Backward compatibility: if no channels configured, use default empty channel
		channels = []string{""}
	}
	
	slog.Info("starting workers", slog.Int("channel_count", len(channels)), slog.Any("channels", channels))
	
	for _, ch := range channels {
		channel := ch // capture for goroutine
		// Start per-channel jobs
		// Chat recorder: either auto mode (poll live) or manual recorder when env has fixed VOD id/start
		if os.Getenv("CHAT_AUTO_START") == "1" {
			go chat.StartAutoChatRecorder(ctx, database, channel)
		} else if err := cfg.ValidateChatReady(); err == nil && channel == os.Getenv("TWITCH_CHANNEL") {
			// Manual recorder only for the configured TWITCH_CHANNEL
			go chat.StartTwitchChatRecorder(ctx, database, cfg.TwitchVODID, cfg.TwitchVODStart)
		} else if channel == "" && len(channels) == 1 {
			slog.Info("chat recorder disabled (missing twitch creds or auto not enabled)")
		}
		go vod.StartVODProcessingJob(ctx, database, channel)
		go vod.StartVODCatalogBackfillJob(ctx, database, channel)
		go vod.StartRetentionJob(ctx, database, channel)
	}

	// Centralized OAuth token refreshers
	oauth.StartRefresher(ctx, database, "twitch", 5*time.Minute, 15*time.Minute, func(rctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		res, err := twitchapi.RefreshToken(rctx, cfg.TwitchClientID, cfg.TwitchClientSecret, refreshToken)
		if err != nil {
			return "", "", time.Time{}, "", err
		}
		return res.AccessToken, res.RefreshToken, twitchapi.ComputeExpiry(res.ExpiresIn), strings.Join(res.Scope, " "), nil
	})
	oauth.StartRefresher(ctx, database, "youtube", 10*time.Minute, 20*time.Minute, func(rctx context.Context, refreshToken string) (string, string, time.Time, string, error) {
		cfg2, _ := config.Load()
		ts := &oauth2.Token{RefreshToken: refreshToken}
		if cfg2.YTClientID == "" {
			return "", "", time.Time{}, "", context.Canceled
		}
		oc := &oauth2.Config{ClientID: cfg2.YTClientID, ClientSecret: cfg2.YTClientSecret, Endpoint: google.Endpoint, RedirectURL: cfg2.YTRedirectURI}
		newTok, err := oc.TokenSource(rctx, ts).Token()
		if err != nil {
			return "", "", time.Time{}, "", err
		}
		return newTok.AccessToken, newTok.RefreshToken, newTok.Expiry, "", nil
	})

	// Enable pprof profiling endpoints in debug mode (ENABLE_PPROF=1)
	if os.Getenv("ENABLE_PPROF") == "1" {
		pprofAddr := os.Getenv("PPROF_ADDR")
		if pprofAddr == "" {
			pprofAddr = "localhost:6060"
		}
		go func() {
			slog.Info("pprof profiling enabled", slog.String("addr", pprofAddr))
			// Use an http.Server with timeouts to satisfy G114 and avoid DoS risks
			srv := &http.Server{
				Addr:              pprofAddr,
				Handler:           nil, // default mux exposes /debug/pprof
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       10 * time.Second,
				WriteTimeout:      10 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
			if err := srv.ListenAndServe(); err != nil {
				slog.Error("pprof server error", slog.Any("err", err))
			}
		}()
	}

	// HTTP server (health/status/metrics)
	// Allow config override via kv (cfg:HTTP_ADDR) if set through the admin API
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
