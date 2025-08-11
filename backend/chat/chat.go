package chat

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	twitch "github.com/gempir/go-twitch-irc/v4"
)

// StartTwitchChatRecorder records chat for a given VOD, with VOD ID and VOD start time for replay accuracy.
func StartTwitchChatRecorder(ctx context.Context, db *sql.DB, vodID string, vodStart time.Time) {
	channel := os.Getenv("TWITCH_CHANNEL")
	username := os.Getenv("TWITCH_BOT_USERNAME")
	oauth := os.Getenv("TWITCH_OAUTH_TOKEN")
	if channel == "" || username == "" || oauth == "" {
		slog.Info("twitch creds not set; skipping chat recorder")
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
		replyToID := msg.ReplyParentMsgID
		replyToUsername := msg.ReplyParentUserName
		replyToMessage := msg.ReplyParentMsgBody
		if _, err := db.Exec(`INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color, reply_to_id, reply_to_username, reply_to_message) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			vodID, msg.User.Name, msg.Message, absTime, relTime, badges, emotes, color, replyToID, replyToUsername, replyToMessage); err != nil {
			slog.Error("failed to insert chat message", slog.Any("err", err))
		}
	})

	// Handle context cancellation by closing the client
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		client.Disconnect()
		close(done)
	}()

	client.Join(channel)
	if err := client.Connect(); err != nil {
		slog.Error("twitch chat connect error", slog.Any("err", err))
	}
	<-done
}
