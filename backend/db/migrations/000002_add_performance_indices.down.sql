-- Rollback performance optimization indices

BEGIN;

DROP INDEX IF EXISTS idx_vods_downloading;
DROP INDEX IF EXISTS idx_chat_messages_abs_timestamp;
DROP INDEX IF EXISTS idx_vods_channel_processed_priority;

COMMIT;
