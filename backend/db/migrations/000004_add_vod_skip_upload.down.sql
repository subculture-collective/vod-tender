-- Rollback per-VOD upload control flag.

BEGIN;

ALTER TABLE vods
    DROP COLUMN IF EXISTS skip_upload;

COMMIT;
