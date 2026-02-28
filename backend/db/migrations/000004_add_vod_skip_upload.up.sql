-- Add per-VOD upload control flag.

BEGIN;

ALTER TABLE vods
    ADD COLUMN IF NOT EXISTS skip_upload BOOLEAN NOT NULL DEFAULT FALSE;

COMMIT;
