-- Performance optimization: Additional indices for common query patterns
-- Using CREATE INDEX CONCURRENTLY for zero-downtime index creation

-- Partial index for active processing (downloading/pending states)
-- This speeds up queries that check download_state for active downloads
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_vods_downloading 
    ON vods(twitch_vod_id, download_state)
    WHERE download_state IN ('downloading', 'pending');

-- Index for querying chat messages by absolute timestamp
-- Useful for time-based chat replay queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_messages_abs_timestamp 
    ON chat_messages(abs_timestamp DESC);

-- Composite index for VOD processing queries
-- Optimizes queries that filter by channel and processed status with ordering
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_vods_channel_processed_priority 
    ON vods(channel, processed, priority DESC, date ASC)
    WHERE processed = false;
