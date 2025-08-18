// Package chat contains the Twitch chat recorder and the auto-orchestrator.
//
// It provides two entrypoints:
//   - StartTwitchChatRecorder: connects to Twitch IRC for TWITCH_CHANNEL and
//     persists messages into the chat_messages table, using both absolute and
//     relative (to VOD start) timestamps for replay.
//   - StartAutoChatRecorder: polls Twitch live status and automatically starts
//     the recorder when the channel goes live. While live, messages are stored
//     under a placeholder VOD id (e.g. "live-<unix>"). After the stream ends,
//     the code reconciles the placeholder with the real published VOD and
//     adjusts relative timestamps if the actual start time differs.
//
// Credentials: the IRC client requires a bot username and an OAuth token with
// chat:read/chat:edit scopes. If TWITCH_OAUTH_TOKEN is not provided, the
// package will try to reuse a stored token from the oauth_tokens table for
// provider "twitch".
package chat
