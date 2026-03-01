-- Add chat privacy support columns and indexes.

BEGIN;

ALTER TABLE chat_messages
    ADD COLUMN IF NOT EXISTS username_hash TEXT;

ALTER TABLE chat_messages
    ADD COLUMN IF NOT EXISTS anonymized_at TIMESTAMPTZ;

-- Retention and anonymization jobs operate by channel + timestamp/hash.
CREATE INDEX IF NOT EXISTS idx_chat_messages_channel_abs_timestamp
    ON chat_messages(channel, abs_timestamp);

CREATE INDEX IF NOT EXISTS idx_chat_messages_channel_username_hash
    ON chat_messages(channel, username_hash);

COMMIT;
