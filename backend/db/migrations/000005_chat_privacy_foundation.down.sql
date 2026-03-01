-- Rollback chat privacy support columns and indexes.

BEGIN;

DROP INDEX IF EXISTS idx_chat_messages_channel_username_hash;
DROP INDEX IF EXISTS idx_chat_messages_channel_abs_timestamp;

ALTER TABLE chat_messages
    DROP COLUMN IF EXISTS anonymized_at;

ALTER TABLE chat_messages
    DROP COLUMN IF EXISTS username_hash;

COMMIT;
