package chat

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
)

// StartTwitchChatRecorder records chat for a given VOD, with VOD ID and VOD start time for replay accuracy.
func StartTwitchChatRecorder(db *sql.DB, vodID string, vodStart time.Time) {
	channel := os.Getenv("TWITCH_CHANNEL")
	username := os.Getenv("TWITCH_BOT_USERNAME")
	oauth := os.Getenv("TWITCH_OAUTH_TOKEN")
	if channel == "" || username == "" || oauth == "" {
		log.Println("Twitch credentials not set, skipping chat recorder")
		return
	}
	client := twitch.NewClient(username, oauth)
	client.OnPrivateMessage(func(msg twitch.PrivateMessage) {
		absTime := time.Now().UTC()
		relTime := absTime.Sub(vodStart).Seconds()
		badges := ""
		if len(msg.User.Badges) > 0 {
			for k, v := range msg.User.Badges {
				badges += k + ":" + fmt.Sprintf("%v", v) + ","
			}
		}
		emotes := ""
		if len(msg.Emotes) > 0 {
			for _, e := range msg.Emotes {
				emotes += e.Name + ","
			}
		}
		color := msg.User.Color
		_, err := db.Exec(`INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			vodID, msg.User.Name, msg.Message, absTime, relTime, badges, emotes, color)
		if err != nil {
			log.Printf("failed to insert chat message: %v", err)
		}
	})
	client.Join(channel)
	err := client.Connect()
	if err != nil {
		log.Printf("Twitch chat connect error: %v", err)
	}
}
