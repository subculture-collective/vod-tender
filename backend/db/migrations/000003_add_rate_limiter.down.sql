-- Rollback rate limiter table

BEGIN;

DROP TABLE IF EXISTS rate_limit_requests CASCADE;

COMMIT;
