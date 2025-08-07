package main

import (
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/onnwee/vod-tender/backend/chat"
	"github.com/onnwee/vod-tender/backend/db"
	"github.com/onnwee/vod-tender/backend/vod"
)

func main() {
	// Load .env file if present
	_ = godotenv.Load("backend/.env")

	database, err := db.Connect()
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		log.Fatalf("failed to migrate db: %v", err)
	}

	// Example: get VOD ID and start time from environment variables
	vodID := os.Getenv("TWITCH_VOD_ID")
	vodStartStr := os.Getenv("TWITCH_VOD_START") // RFC3339 format
	var vodStart time.Time
	if vodStartStr != "" {
		t, err := time.Parse(time.RFC3339, vodStartStr)
		if err != nil {
			log.Fatalf("Invalid VOD start time: %v", err)
		}
		vodStart = t
	} else {
		vodStart = time.Now().UTC()
	}
	if vodID == "" {
		vodID = "demo-vod-id"
	}

	go chat.StartTwitchChatRecorder(database, vodID, vodStart)
	go vod.StartVODProcessingJob(database)

	select {}
}

