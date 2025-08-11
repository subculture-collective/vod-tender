package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/onnwee/vod-tender/backend/chat"
	"github.com/onnwee/vod-tender/backend/config"
	"github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/server"
	"github.com/onnwee/vod-tender/backend/vod"
)

func main() {
	// Load .env file if present
	_ = godotenv.Load("backend/.env")

	// Config
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", slog.Any("err", err))
		os.Exit(1)
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
	// Chat recorder is optional; only start if creds are present
	if err := cfg.ValidateChatReady(); err == nil {
		go chat.StartTwitchChatRecorder(ctx, database, cfg.TwitchVODID, cfg.TwitchVODStart)
	} else {
		slog.Info("chat recorder disabled (missing twitch creds)")
	}
	go vod.StartVODProcessingJob(ctx, database)

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

