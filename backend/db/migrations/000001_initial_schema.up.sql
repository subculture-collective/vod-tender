-- Initial schema migration extracted from db.go
-- This represents the current state of the database schema

BEGIN;

-- Create vods table
CREATE TABLE IF NOT EXISTS vods (
    id SERIAL PRIMARY KEY,
    twitch_vod_id TEXT UNIQUE,
    title TEXT,
    date TIMESTAMPTZ,
    duration_seconds INTEGER,
    downloaded_path TEXT,
    download_state TEXT,
    download_retries INTEGER DEFAULT 0,
    download_bytes BIGINT DEFAULT 0,
    download_total BIGINT DEFAULT 0,
    progress_updated_at TIMESTAMPTZ,
    processed BOOLEAN DEFAULT FALSE,
    processing_error TEXT,
    youtube_url TEXT,
    description TEXT,
    priority INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ,
    channel TEXT NOT NULL DEFAULT ''
);

-- Create chat_messages table
CREATE TABLE IF NOT EXISTS chat_messages (
    id SERIAL PRIMARY KEY,
    vod_id TEXT NOT NULL REFERENCES vods(twitch_vod_id),
    username TEXT,
    message TEXT,
    abs_timestamp TIMESTAMPTZ,
    rel_timestamp DOUBLE PRECISION,
    badges TEXT,
    emotes TEXT,
    color TEXT,
    reply_to_id TEXT,
    reply_to_username TEXT,
    reply_to_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    channel TEXT NOT NULL DEFAULT ''
);

-- Create oauth_tokens table with composite primary key for multi-channel support
CREATE TABLE IF NOT EXISTS oauth_tokens (
    provider TEXT NOT NULL,
    channel TEXT NOT NULL DEFAULT '',
    access_token TEXT,
    refresh_token TEXT,
    expires_at TIMESTAMPTZ,
    scope TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    encryption_version INTEGER DEFAULT 0,
    encryption_key_id TEXT,
    PRIMARY KEY (provider, channel)
);

-- Create kv table with composite primary key for multi-channel support
CREATE TABLE IF NOT EXISTS kv (
    channel TEXT NOT NULL DEFAULT '',
    key TEXT NOT NULL,
    value TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (channel, key)
);

-- Create basic indices
CREATE INDEX IF NOT EXISTS idx_vods_twitch_vod_id ON vods(twitch_vod_id);
CREATE INDEX IF NOT EXISTS idx_vods_processed ON vods(processed);
CREATE INDEX IF NOT EXISTS idx_vods_date ON vods(date);
CREATE INDEX IF NOT EXISTS idx_vods_proc_pri_date ON vods(processed, priority, date);
CREATE INDEX IF NOT EXISTS idx_chat_vod_rel ON chat_messages(vod_id, rel_timestamp);
CREATE INDEX IF NOT EXISTS idx_chat_vod_abs ON chat_messages(vod_id, abs_timestamp);

-- Multi-channel support indices
CREATE INDEX IF NOT EXISTS idx_vods_channel_date ON vods(channel, date DESC);
CREATE INDEX IF NOT EXISTS idx_vods_channel_processed ON vods(channel, processed, priority DESC, date ASC);
CREATE INDEX IF NOT EXISTS idx_chat_messages_channel_vod ON chat_messages(channel, vod_id);

COMMIT;
