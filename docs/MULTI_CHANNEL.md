# Multi-Channel Support

vod-tender now supports managing multiple Twitch channels in a single deployment.

## Overview

Instead of running multiple Docker Compose stacks (one per channel), you can now configure multiple channels in a single instance. Each channel gets:

- Isolated database records (VODs, chat messages, OAuth tokens, kv state)
- Dedicated processing goroutines
- Dedicated catalog backfill goroutines
- Dedicated chat recorders (in auto mode)
- Independent circuit breaker state
- Separate performance metrics

## Configuration

### Single Channel (Backward Compatible)

The existing single-channel mode continues to work without changes:

```bash
# backend/.env
TWITCH_CHANNEL=mychannel
TWITCH_BOT_USERNAME=mybotuser
TWITCH_OAUTH_TOKEN=oauth:...
TWITCH_CLIENT_ID=...
TWITCH_CLIENT_SECRET=...
```

### Multi-Channel Mode

Use the `TWITCH_CHANNELS` environment variable with a comma-separated list:

```bash
# backend/.env
TWITCH_CHANNELS=channel1,channel2,channel3
TWITCH_BOT_USERNAME=mybotuser
TWITCH_OAUTH_TOKEN=oauth:...
TWITCH_CLIENT_ID=...
TWITCH_CLIENT_SECRET=...
```

**Note:** Currently, all channels share the same bot credentials and client ID/secret. Per-channel OAuth tokens are supported in the database schema but require additional API endpoints for configuration (planned for future PR).

## How It Works

### Startup

When the application starts:

1. `config.Load()` parses `TWITCH_CHANNELS` into a list
2. Falls back to single `TWITCH_CHANNEL` if `TWITCH_CHANNELS` is not set
3. `main.go` spawns goroutines for each channel:
   - `vod.StartVODProcessingJob(ctx, db, channel)`
   - `vod.StartVODCatalogBackfillJob(ctx, db, channel)`
   - `chat.StartAutoChatRecorder(ctx, db, channel)` (if `CHAT_AUTO_START=1`)

### Channel Isolation

All database queries are filtered by channel:

```sql
-- Example: Get unprocessed VODs for a specific channel
SELECT * FROM vods 
WHERE channel = $1 
  AND processed = false 
ORDER BY priority DESC, date ASC;

-- Example: Get circuit breaker state for a channel
SELECT value FROM kv 
WHERE channel = $1 
  AND key = 'circuit_state';
```

### Circuit Breaker Isolation

Each channel has its own circuit breaker state in the `kv` table:

```sql
-- Channel A circuit state
INSERT INTO kv (channel, key, value, updated_at) 
VALUES ('channelA', 'circuit_state', 'closed', NOW());

-- Channel B circuit state (independent)
INSERT INTO kv (channel, key, value, updated_at) 
VALUES ('channelB', 'circuit_state', 'open', NOW());
```

If one channel's circuit breaker opens (due to persistent failures), other channels continue processing normally.

## Database Schema

### Channel Column

All major tables have a `channel` column with `DEFAULT ''` for backward compatibility:

- `vods.channel`
- `chat_messages.channel`
- `oauth_tokens.channel`
- `kv.channel`

### Primary Key Changes

Two tables have updated composite primary keys:

```sql
-- oauth_tokens: (provider, channel)
ALTER TABLE oauth_tokens 
  DROP CONSTRAINT oauth_tokens_pkey,
  ADD PRIMARY KEY (provider, channel);

-- kv: (channel, key)
ALTER TABLE kv 
  DROP CONSTRAINT kv_pkey,
  ADD PRIMARY KEY (channel, key);
```

### Indices for Performance

New indices optimize channel-based queries:

```sql
CREATE INDEX idx_vods_channel_date 
  ON vods(channel, date DESC);

CREATE INDEX idx_vods_channel_processed 
  ON vods(channel, processed, priority DESC, date ASC);

CREATE INDEX idx_chat_messages_channel_vod 
  ON chat_messages(channel, vod_id);
```

## Admin API

Admin endpoints support an optional `?channel=` query parameter:

```bash
# Scan VODs for a specific channel
curl -X POST "http://localhost:8080/admin/vod/scan?channel=channel1"

# Backfill catalog for a specific channel
curl -X POST "http://localhost:8080/admin/vod/catalog?channel=channel2&max=100"

# If channel is omitted, defaults to TWITCH_CHANNEL env var
curl -X POST "http://localhost:8080/admin/vod/scan"
```

## Migration from Single to Multi-Channel

### Existing Single-Channel Deployment

If you have an existing single-channel deployment with data:

1. **No action required** - Your existing data uses `channel = ''` (empty string)
2. The system continues to work as before
3. Queries for empty channel will find your existing data

### Adding Additional Channels

To add more channels to an existing deployment:

1. Update `backend/.env`:
   ```bash
   # Before
   TWITCH_CHANNEL=oldchannel
   
   # After
   TWITCH_CHANNELS=oldchannel,newchannel1,newchannel2
   ```

2. Restart the service:
   ```bash
   make restart
   ```

3. Verify all channels are running:
   ```bash
   # Check logs for each channel
   make logs-backend | grep "vod processing job starting"
   make logs-backend | grep "catalog backfill job starting"
   make logs-backend | grep "auto chat: started poller"
   ```

### Data Separation

Each new channel starts with a clean slate:
- No VODs until catalog backfill runs
- No chat history
- Independent circuit breaker state
- Fresh OAuth tokens (if configured per-channel in the future)

Existing data (channel = '') remains accessible and continues to be processed.

## Monitoring

### Logs

Each channel's goroutines log with a `channel` attribute:

```
vod processing job starting interval=1m0s channel=channel1
vod processing job starting interval=1m0s channel=channel2
catalog backfill job starting interval=6h0m0s max=0 max_age=0s channel=channel1
catalog backfill job starting interval=6h0m0s max=0 max_age=0s channel=channel2
```

### Metrics

Current implementation shares global Prometheus metrics. Channel-scoped metrics planned for future PR.

### Health Checks

The `/healthz` endpoint reports overall health. Per-channel health status API planned for future PR.

## Limitations

### Current Implementation

1. **Shared credentials**: All channels use the same Twitch bot username and client credentials
2. **No dynamic management**: Channels must be configured at startup; adding/removing requires restart
3. **Global metrics**: Prometheus metrics not yet scoped by channel
4. **No rate limiting**: Helix API rate limits not shared across channels (each channel makes independent requests)

### Future Enhancements (Planned)

- Dynamic channel management API (add/remove without restart)
- Per-channel OAuth token configuration
- Channel-scoped Prometheus metrics
- Global rate limiter for Helix API
- Download slot allocation with fair scheduling
- Supervisor pattern with hot-reload

## Troubleshooting

### Channel Not Processing

Check logs for the specific channel:

```bash
make logs-backend | grep "channel=mychannel"
```

Verify the channel name is correct (case-sensitive).

### Circuit Breaker Stuck Open

Check circuit state for each channel:

```sql
SELECT channel, key, value 
FROM kv 
WHERE key = 'circuit_state';
```

Manually reset if needed:

```sql
UPDATE kv 
SET value = 'closed' 
WHERE channel = 'mychannel' 
  AND key = 'circuit_state';
```

### OAuth Token Issues

Currently, OAuth tokens are per-channel but must be configured manually in the database:

```sql
-- View tokens
SELECT provider, channel, expires_at 
FROM oauth_tokens;

-- Note: Per-channel token configuration API coming in future PR
```

## Example: Three-Channel Deployment

```bash
# backend/.env
TWITCH_CHANNELS=streamer1,streamer2,streamer3
TWITCH_BOT_USERNAME=vodbot
TWITCH_CLIENT_ID=abc123
TWITCH_CLIENT_SECRET=xyz789
CHAT_AUTO_START=1

# Start the service
make up

# Watch logs for all channels
make logs-backend

# Expected output:
# vod processing job starting interval=1m0s channel=streamer1
# vod processing job starting interval=1m0s channel=streamer2
# vod processing job starting interval=1m0s channel=streamer3
# catalog backfill job starting channel=streamer1
# catalog backfill job starting channel=streamer2
# catalog backfill job starting channel=streamer3
# auto chat: started poller interval=30s
# ...
```

Each channel independently:
- Polls for new VODs every 6 hours
- Processes unprocessed VODs every 1 minute
- Records live chat (if streaming)
- Maintains its own circuit breaker state

## Resources

- [Original Issue](https://github.com/subculture-collective/vod-tender/issues/XX)
- [Database Schema](../backend/db/db.go)
- [Configuration](../backend/config/config.go)
- [Main Orchestration](../backend/main.go)
