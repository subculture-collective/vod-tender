-- Rollback initial schema migration
-- WARNING: This will drop all tables and data

BEGIN;

DROP TABLE IF EXISTS chat_messages CASCADE;
DROP TABLE IF EXISTS vods CASCADE;
DROP TABLE IF EXISTS oauth_tokens CASCADE;
DROP TABLE IF EXISTS kv CASCADE;

COMMIT;
