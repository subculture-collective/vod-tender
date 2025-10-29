# OAuth Token Migration Tool

CLI tool to migrate OAuth tokens from plaintext (encryption_version=0) to encrypted storage (encryption_version=1) using AES-256-GCM.

## Overview

This tool safely migrates existing plaintext OAuth tokens to encrypted format. It's designed to be:
- **Safe**: Dry-run mode lets you preview changes
- **Atomic**: Each token migrated in its own transaction
- **Idempotent**: Safe to run multiple times
- **Resumable**: Continues on errors, reports summary

## Prerequisites

- `DB_DSN`: Database connection string
- `ENCRYPTION_KEY`: Base64-encoded 32-byte encryption key

## Quick Start

### 1. Generate Encryption Key (if not already set)

```bash
openssl rand -base64 32
```

### 2. Set Environment Variables

```bash
export DB_DSN="postgres://vod:vod@localhost:5432/vod?sslmode=disable"
export ENCRYPTION_KEY="your-base64-key-here"
```

### 3. Preview Migration (Dry-Run)

```bash
# Build
go build -o migrate-tokens .

# Preview what would be migrated
./migrate-tokens --dry-run
```

Expected output:
```
level=INFO msg="found plaintext tokens to migrate" count=3 dry_run=true
level=INFO msg="would migrate token (dry-run)" provider=twitch channel= index=1 total=3
level=INFO msg="would migrate token (dry-run)" provider=youtube channel= index=2 total=3
level=INFO msg="would migrate token (dry-run)" provider=twitch channel=channel-a index=3 total=3
level=INFO msg="migration summary" total=3 migrated=3 errors=0 dry_run=true
level=INFO msg="migration completed successfully"
```

### 4. Run Migration

```bash
./migrate-tokens
```

Expected output:
```
level=INFO msg="found plaintext tokens to migrate" count=3 dry_run=false
level=INFO msg="migrated token successfully" provider=twitch channel= index=1 total=3
level=INFO msg="migrated token successfully" provider=youtube channel= index=2 total=3
level=INFO msg="migrated token successfully" provider=twitch channel=channel-a index=3 total=3
level=INFO msg="migration summary" total=3 migrated=3 errors=0 dry_run=false
level=INFO msg="migration completed successfully"
```

### 5. Verify Migration

```bash
# Connect to database
psql $DB_DSN

# Check encryption status
SELECT provider, channel, encryption_version, encryption_key_id 
FROM oauth_tokens;
```

All tokens should show `encryption_version = 1` and `encryption_key_id = 'default'`.

## Usage

```
migrate-tokens [flags]

Flags:
  --dry-run         Show what would be migrated without making changes
  --channel string  Migrate tokens for specific channel only (default: all channels)
```

### Examples

```bash
# Migrate all tokens
./migrate-tokens

# Migrate specific channel only
./migrate-tokens --channel "my-channel"

# Preview migration for specific channel
./migrate-tokens --dry-run --channel "my-channel"
```

## Docker Usage

### Docker Compose

```bash
# Build the migration tool in the container
docker compose exec api go build -o /app/migrate-tokens ./cmd/migrate-tokens

# Run dry-run
docker compose exec api /app/migrate-tokens --dry-run

# Run migration
docker compose exec api /app/migrate-tokens
```

### Kubernetes

```bash
# Build and run migration in a pod
kubectl exec -it deployment/vod-tender-backend -- sh

# Inside the pod
cd /app
go build -o migrate-tokens ./cmd/migrate-tokens
./migrate-tokens --dry-run
./migrate-tokens
```

Alternatively, use the pre-built binary if available:

```bash
kubectl exec -it deployment/vod-tender-backend -- /app/migrate-tokens --dry-run
kubectl exec -it deployment/vod-tender-backend -- /app/migrate-tokens
```

## Migration Process

The tool performs these steps for each plaintext token:

1. Query database for tokens where `encryption_version = 0`
2. For each token:
   - Start transaction
   - Encrypt `access_token` with AES-256-GCM
   - Encrypt `refresh_token` with AES-256-GCM
   - Update database with encrypted tokens
   - Set `encryption_version = 1`
   - Set `encryption_key_id = 'default'`
   - Commit transaction
3. Report summary (total, migrated, errors)

## Error Handling

- **Individual token failures**: Logged but don't stop migration
- **Database connection errors**: Migration aborts immediately
- **Encryption errors**: Token skipped, logged, migration continues
- **Concurrent modifications**: Transaction rolls back, token skipped

Example error output:
```
level=ERROR msg="failed to migrate token" provider=twitch channel=test error="encrypt access token: ..."
level=INFO msg="migration summary" total=5 migrated=4 errors=1 dry_run=false
```

## Safety Features

### Atomic Updates
Each token migration is wrapped in a transaction. If any step fails, the transaction rolls back and the token remains in its original state.

### Idempotent
Running the tool multiple times is safe. Tokens already at `encryption_version = 1` are automatically skipped.

### Dry-Run Mode
Always run with `--dry-run` first to preview changes:
- Shows what would be migrated
- Does not modify database
- Useful for planning and validation

### Verification Query
Built-in WHERE clause ensures only plaintext tokens are processed:
```sql
WHERE encryption_version = 0
```

## Troubleshooting

### No tokens found

```
level=INFO msg="no plaintext tokens found to migrate"
```

**Cause**: All tokens are already encrypted (version=1) or no tokens exist.

**Solution**: Verify with:
```sql
SELECT encryption_version, COUNT(*) FROM oauth_tokens GROUP BY encryption_version;
```

### Encryption key error

```
level=ERROR msg="failed to initialize encryptor" error="invalid encryption key: must be 32 bytes"
```

**Cause**: `ENCRYPTION_KEY` is not a valid base64-encoded 32-byte key.

**Solution**: Generate new key:
```bash
openssl rand -base64 32
```

### Database connection error

```
level=ERROR msg="failed to connect to database" error="dial tcp: connection refused"
```

**Cause**: Database is not accessible or `DB_DSN` is incorrect.

**Solution**: 
- Check database is running
- Verify `DB_DSN` format: `postgres://user:pass@host:port/db?sslmode=...`
- Test connection: `psql "$DB_DSN"`

### Token already migrated

```
level=ERROR msg="failed to migrate token" error="expected 1 row updated, got 0"
```

**Cause**: Token was modified concurrently (e.g., refreshed while migration running).

**Solution**: This is safe to ignore. Token was already updated by another process.

## Rollback

If you need to rollback to plaintext tokens:

1. **Stop the application** to prevent new encrypted tokens from being written
2. **Decrypt tokens** manually:
   ```sql
   -- This requires decrypting tokens outside the database
   -- Usually not recommended; consider fixing encryption key instead
   ```
3. **Update encryption version**:
   ```sql
   UPDATE oauth_tokens SET encryption_version = 0 WHERE encryption_version = 1;
   ```
4. **Remove encryption key** from environment
5. **Restart application**

⚠️ **Warning**: Rollback is rarely needed. If migration fails, fix the issue and re-run the tool.

## Best Practices

### Before Migration
1. ✅ Backup database
2. ✅ Run dry-run mode first
3. ✅ Verify encryption key is correctly set
4. ✅ Test in staging environment first

### During Migration
1. ✅ Application can stay running (backward compatible)
2. ✅ Monitor logs for errors
3. ✅ Verify success with database query

### After Migration
1. ✅ Verify all tokens encrypted (version=1)
2. ✅ Test OAuth token refresh still works
3. ✅ Store encryption key securely (Secrets Manager, Vault)
4. ✅ Document key location for team

## Performance

- **Typical speed**: ~10-50 tokens/second
- **Bottleneck**: Database transaction latency
- **Concurrent safety**: Safe to run while application is running
- **Resource usage**: Minimal (CPU, memory, network)

## Security Notes

- ✅ Encryption key never logged
- ✅ Plaintext tokens never logged
- ✅ Encrypted tokens stored as base64
- ✅ AES-256-GCM provides authentication (AEAD)
- ✅ Random nonce per encryption
- ⚠️ Protect encryption key like production database password

## Related Documentation

- [OPERATIONS.md](../../../docs/OPERATIONS.md) - Migration procedures
- [CONFIG.md](../../../docs/CONFIG.md) - Encryption key setup
- [crypto package](../../crypto/) - Encryption implementation
- [db package](../../db/) - Database token storage

## Support

For issues or questions:
1. Check troubleshooting section above
2. Review logs for error details
3. Verify environment variables are set
4. Test database connectivity
5. Consult team documentation
