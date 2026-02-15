-- Add rate limiter table for distributed rate limiting across API replicas
-- This table tracks request timestamps per IP for sliding window rate limiting

BEGIN;

-- Create rate_limit_requests table
CREATE TABLE IF NOT EXISTS rate_limit_requests (
    id BIGSERIAL PRIMARY KEY,
    ip TEXT NOT NULL,
    request_time TIMESTAMPTZ NOT NULL
);

-- Index for efficient rate limit lookups by IP and time window
CREATE INDEX IF NOT EXISTS idx_rate_limit_ip_time 
    ON rate_limit_requests(ip, request_time);

-- Index for time-based cleanup of old entries
CREATE INDEX IF NOT EXISTS idx_rate_limit_time 
    ON rate_limit_requests(request_time);

COMMIT;
