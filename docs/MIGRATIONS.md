# Database Migration Architecture

This document explains the database migration system used in vod-tender and provides guidance for creating and managing migrations.

## Overview

vod-tender uses **golang-migrate** as the canonical migration system with a legacy embedded SQL fallback for backward compatibility.

### Migration Systems

The project has two migration approaches:

1. **golang-migrate (Canonical)** — Versioned migrations in `backend/db/migrations/`
   - Primary migration system for all new schema changes
   - Provides version control, rollback capability, and migration history
   - Tracked in `schema_migrations` table
   - Run via `db.RunMigrations()` in `backend/db/migrate.go`

2. **Embedded SQL (Legacy/Fallback)** — Idempotent statements in `backend/db/db.go`
   - For backward compatibility with pre-migration deployments
   - Single-pass schema setup using `CREATE TABLE IF NOT EXISTS` patterns
   - No version tracking or rollback capability
   - Run via `db.Migrate()` in `backend/db/db.go`
   - **Deprecated for new schema changes**

### Execution Order

When the application starts (`backend/main.go`), migrations run in this order:

```go
1. db.RunMigrations() — Try golang-migrate versioned migrations first
2. If step 1 fails → db.Migrate() — Fallback to legacy embedded SQL
3. Log success/failure and continue startup
```

This approach ensures:
- New deployments use versioned migrations with full tracking
- Old deployments without `schema_migrations` table fall back gracefully
- Smooth transition from legacy to versioned migrations
- Zero-downtime upgrades for existing instances

## Design Rationale

### Why Two Systems?

The dual system exists for **backward compatibility**:

- **Legacy behavior**: Original deployments used embedded SQL (`db.Migrate()`) with no migration tracking
- **Modern approach**: New deployments use golang-migrate for proper version control and rollback
- **Transition period**: Fallback ensures existing instances upgrade smoothly without manual intervention

### Which to Use?

| Scenario | Use |
|----------|-----|
| Creating new migrations | **golang-migrate** (versioned migrations in `db/migrations/`) |
| Modifying schema | **golang-migrate** (create new versioned migration) |
| Upgrading existing deployment | Automatic (versioned migrations run first, fallback if needed) |
| Fresh installation | **golang-migrate** (embedded SQL not needed) |

**Rule of thumb**: Always use golang-migrate for schema changes. Never modify embedded SQL in `db.Migrate()` unless absolutely necessary for backward compatibility.

## Migration Coverage

### Current State

Both systems provide equivalent schema coverage:

#### Tables (Covered by both)
- `vods` — VOD metadata, download state, processing status
- `chat_messages` — Recorded chat messages linked to VODs
- `oauth_tokens` — Encrypted OAuth tokens (Twitch, YouTube)
- `kv` — Key-value store for circuit breaker, EMAs, etc.

#### Recently Migrated Tables
- `rate_limit_requests` — Distributed rate limiting across API replicas
  - ✅ **Migrated in 000003_add_rate_limiter.up.sql**

#### Indices
- **Versioned migrations**: Basic indices (vods, chat, channels) + performance indices + rate limiter indices
- **Embedded SQL**: Same indices
- ✅ **No action needed**: Schema coverage is complete

### Schema Drift Prevention

To prevent schema drift between the two systems:

1. **CI Tests** — `database-migration-tests` job in `.github/workflows/ci.yml` verifies:
   - Migrations apply cleanly to empty database
   - Migrations are idempotent (safe to run multiple times)
   - Forward and backward migration works
   - Data is preserved during migrations

2. **Test Coverage** — `backend/db/migrate_test.go` and `migration_idempotency_test.go` verify:
   - All tables created after migrations
   - Primary keys and constraints correct
   - Old schema upgrades to new schema (multi-channel support)

3. **Documentation** — This file documents expected schema state

## Creating Migrations

### Prerequisites

Install golang-migrate CLI:

```bash
make migrate-install
# OR manually:
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

### Create New Migration

```bash
# Using Makefile (recommended)
make migrate-create name=add_something

# Using migrate CLI directly
cd backend
migrate create -ext sql -dir db/migrations -seq add_something
```

This creates two files:
- `000XXX_add_something.up.sql` — Applies the migration
- `000XXX_add_something.down.sql` — Reverts the migration

### Writing Migrations

Follow these best practices:

#### 1. Always Reversible

Every `.up.sql` migration MUST have a working `.down.sql` migration:

```sql
-- 000003_add_user_column.up.sql
BEGIN;

ALTER TABLE vods ADD COLUMN IF NOT EXISTS user_id TEXT;
CREATE INDEX IF NOT EXISTS idx_vods_user_id ON vods(user_id);

COMMIT;
```

```sql
-- 000003_add_user_column.down.sql
BEGIN;

DROP INDEX IF EXISTS idx_vods_user_id;
ALTER TABLE vods DROP COLUMN IF EXISTS user_id;

COMMIT;
```

#### 2. Idempotent

Use `IF NOT EXISTS` / `IF EXISTS` to make migrations safe to retry:

```sql
-- Good
CREATE TABLE IF NOT EXISTS new_table (...);
CREATE INDEX IF NOT EXISTS idx_name ON table(column);
ALTER TABLE table ADD COLUMN IF NOT EXISTS col TEXT;

-- Bad (fails on retry)
CREATE TABLE new_table (...);
CREATE INDEX idx_name ON table(column);
ALTER TABLE table ADD COLUMN col TEXT;
```

#### 3. Transactional

Wrap DDL statements in `BEGIN`/`COMMIT` for atomicity:

```sql
BEGIN;

-- All DDL here executes atomically
CREATE TABLE ...;
CREATE INDEX ...;
ALTER TABLE ...;

COMMIT;
```

**Exception**: `CREATE INDEX CONCURRENTLY` cannot run in transaction. Use only for zero-downtime production index creation.

#### 4. Small, Focused Changes

One logical change per migration:

- ✅ `000003_add_user_column.sql` — Adds `user_id` column
- ✅ `000004_add_user_index.sql` — Adds index on `user_id`
- ❌ `000003_add_user_stuff.sql` — Adds column, index, and modifies other tables

#### 5. Data Preservation

For schema changes that affect existing data:

```sql
-- Adding NOT NULL column with default
ALTER TABLE vods ADD COLUMN status TEXT NOT NULL DEFAULT 'pending';

-- Backfilling data before adding constraint
UPDATE vods SET user_id = 'unknown' WHERE user_id IS NULL;
ALTER TABLE vods ALTER COLUMN user_id SET NOT NULL;

-- Renaming columns (preserve data)
ALTER TABLE vods RENAME COLUMN old_name TO new_name;
```

### Testing Migrations

#### Local Testing

```bash
# Start test Postgres (avoid conflict with main stack)
docker run -d --name test-postgres \
  -e POSTGRES_USER=vod \
  -e POSTGRES_PASSWORD=vod \
  -e POSTGRES_DB=vod_test \
  -p 5433:5432 \
  postgres:16

# Set test DSN
export TEST_PG_DSN="postgres://vod:vod@localhost:5433/vod_test?sslmode=disable"

# Run migration tests
cd backend
go test ./db -v -run TestMigration

# Cleanup
docker stop test-postgres && docker rm test-postgres
```

#### Manual Testing

```bash
# Set DSN for your docker compose stack
export DSN="postgres://vod:vod@localhost:5469/vod?sslmode=disable"

# Apply migrations
make migrate-up
# OR manually:
migrate -path backend/db/migrations -database "$DSN" up

# Check version
make migrate-status
# OR manually:
migrate -path backend/db/migrations -database "$DSN" version

# Rollback last migration (test .down.sql)
make migrate-down
# OR manually:
migrate -path backend/db/migrations -database "$DSN" down 1

# Re-apply (test idempotency)
make migrate-up
```

#### CI Testing

CI automatically runs migration tests on every PR:

```yaml
# .github/workflows/ci.yml
database-migration-tests:
  - Runs all migration tests in backend/db
  - Verifies clean apply, idempotency, up/down, data preservation
  - Uses fresh Postgres 16 container
```

### Migration Checklist

Before committing a new migration:

- [ ] Created both `.up.sql` and `.down.sql` files
- [ ] Used `IF NOT EXISTS` / `IF EXISTS` for idempotency
- [ ] Wrapped DDL in `BEGIN`/`COMMIT` transaction
- [ ] Tested forward migration (up) locally
- [ ] Tested backward migration (down) locally
- [ ] Tested idempotency (can run up multiple times)
- [ ] Verified data is preserved if applicable
- [ ] Ran `go test ./db -v` successfully
- [ ] Updated this documentation if schema significantly changes

## Managing Migrations

### Apply Migrations

```bash
# Production/staging (automatic on app startup)
# Migrations run in main.go via db.RunMigrations()

# Development (manual)
make migrate-up

# Specific version
migrate -path backend/db/migrations -database "$DSN" goto 2
```

### Rollback Migrations

**WARNING**: Rollback may cause data loss. Only use in development or emergency scenarios.

```bash
# Rollback last migration
make migrate-down

# Rollback to specific version
migrate -path backend/db/migrations -database "$DSN" goto 1

# Rollback all migrations (dangerous!)
migrate -path backend/db/migrations -database "$DSN" down -all
```

### Check Migration Status

```bash
# Using Makefile
make migrate-status

# Using migrate CLI
migrate -path backend/db/migrations -database "$DSN" version

# Query database directly
docker compose exec postgres psql -U vod -d vod -c "SELECT * FROM schema_migrations;"
```

### Dirty State Recovery

If a migration fails mid-execution, the database enters a "dirty" state:

```bash
# 1. Check status
make migrate-status
# Output: "Version: 2 (dirty)"

# 2. Fix the migration file or database manually

# 3. Force version (DANGEROUS - only if you're sure)
make migrate-force VERSION=2

# 4. Try migration again
make migrate-up
```

**Prevention**: Always test migrations locally before deploying.

## Advanced Topics

### Multi-Instance Deployments

Each vod-tender instance (channel) has its own database. Migrations run independently:

```bash
# Instance 1: channelA
STACK_NAME=vod-channelA DB_NAME=vod_channelA make up
# Migrations run on vod_channelA database

# Instance 2: channelB
STACK_NAME=vod-channelB DB_NAME=vod_channelb make up
# Migrations run on vod_channelb database
```

### Zero-Downtime Migrations

For production with minimal downtime:

1. **Backward-compatible changes first**:
   ```sql
   -- Step 1: Add new column (nullable, backward compatible)
   ALTER TABLE vods ADD COLUMN new_col TEXT;
   
   -- Deploy app code that uses new_col if present
   
   -- Step 2: Backfill data (in separate migration after deploy)
   UPDATE vods SET new_col = 'default' WHERE new_col IS NULL;
   
   -- Step 3: Add constraint (in another migration after validation)
   ALTER TABLE vods ALTER COLUMN new_col SET NOT NULL;
   ```

2. **Use CONCURRENTLY for indices** (cannot use transactions):
   ```sql
   -- Create index without blocking writes (not in transaction)
   CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_name ON table(column);
   ```

3. **Test in staging first**: Always test migrations on staging environment before production

### Emergency Procedures

#### Rollback Failed Deployment

```bash
# 1. Identify current version
make migrate-status

# 2. Rollback to previous version
make migrate-down

# 3. Restart app (uses older schema)
make restart
```

#### Force Schema Reset (Development Only)

```bash
# WARNING: Destroys all data
make db-reset

# Migrations re-run automatically on next app start
make up
```

## Troubleshooting

### Migration Path Not Found

**Error**: `migrations directory not found in any of the expected locations`

**Solution**: Run from correct directory:
- From repo root: `make migrate-up` (uses backend/db/migrations)
- From backend/: `migrate -path db/migrations -database "$DSN" up`

### Schema Migrations Table Missing

**Error**: `schema_migrations table does not exist`

**Cause**: Old deployment using embedded SQL only

**Solution**: Automatic fallback to `db.Migrate()` handles this. On next restart, versioned migrations will run.

### Dirty Migration State

**Error**: `Dirty database version X. Fix and force version.`

**Cause**: Migration failed mid-execution

**Solution**:
1. Manually fix database or migration file
2. Force version: `make migrate-force VERSION=X`
3. Retry: `make migrate-up`

### Version Mismatch

**Error**: Version in code doesn't match database

**Cause**: Database modified manually or out-of-sync migration files

**Solution**:
1. Check migration files: `ls backend/db/migrations/`
2. Check database version: `make migrate-status`
3. Apply missing migrations: `make migrate-up`

## Migration History

### Version 1: Initial Schema (000001_initial_schema)

Created all core tables:
- `vods` — VOD metadata and processing state
- `chat_messages` — Chat replay data
- `oauth_tokens` — OAuth credentials (supports encryption)
- `kv` — Key-value store for circuit breaker state

Includes multi-channel support via composite primary keys:
- `oauth_tokens`: PK (provider, channel)
- `kv`: PK (channel, key)

### Version 2: Performance Indices (000002_add_performance_indices)

Added optimizations:
- `idx_vods_downloading` — Partial index for active downloads
- `idx_chat_messages_abs_timestamp` — Time-based chat queries
- `idx_vods_channel_processed_priority` — VOD processing queue optimization

### Version 3: Rate Limiter Table (000003_add_rate_limiter)

Added distributed rate limiting support:
- `rate_limit_requests` — Stores request timestamps per IP for sliding window rate limiting
- `idx_rate_limit_ip_time` — Efficient lookups by IP and time window
- `idx_rate_limit_time` — Time-based cleanup of old entries

This completes the migration of schema from embedded SQL to versioned migrations. All tables and indices are now covered.

### Future Migrations

All core schema is now covered by versioned migrations. Future additions will be added as new migrations following the established pattern.

## Related Documentation

- **Migration Files**: `backend/db/migrations/README.md` — Quick reference for migration commands
- **Database Schema**: `backend/db/db.go` — Legacy embedded SQL (read-only reference)
- **Migration Code**: `backend/db/migrate.go` — golang-migrate integration
- **Migration Tests**: `backend/db/migrate_test.go` — Test suite
- **CI Pipeline**: `.github/workflows/ci.yml` — Automated migration testing
- **Operations**: `docs/OPERATIONS.md` — Deployment and maintenance runbooks
- **Architecture**: `docs/ARCHITECTURE.md` — System design overview

## FAQ

### Should I update db.Migrate() for new schema changes?

**No.** Only modify `backend/db/db.go` if you need to maintain backward compatibility with very old deployments. For all new schema changes, create versioned migrations.

### How do I migrate an existing deployment?

No action needed. On next restart:
1. App tries `db.RunMigrations()` — creates `schema_migrations` table and applies versioned migrations
2. If that succeeds, embedded SQL is skipped
3. Future startups use versioned migrations only

### Can I delete db.Migrate()?

Not yet. It provides fallback for deployments without `schema_migrations` table. After sufficient time (e.g., all deployments migrated), consider deprecating in a future major version.

### What if versioned migrations and embedded SQL diverge?

CI tests prevent this:
- `TestMigrationsIdempotent` verifies embedded SQL is idempotent
- `TestRunMigrations` verifies versioned migrations create correct schema
- Both should produce equivalent schema (minus rate_limit_requests table currently)

If drift occurs, document it here and fix by:
1. Updating versioned migrations to match desired schema
2. Ensuring embedded SQL remains backward compatible
3. Adding deprecation notice

**Current Status**: Schema is in sync. Both systems produce equivalent schema as of version 3 (rate_limiter table migrated).

### How do I handle secrets in migrations?

**Never commit secrets to migration files.** For migrations that need data:

```sql
-- Good: Use placeholder or environment-driven backfill
INSERT INTO oauth_tokens (provider, channel, access_token, refresh_token)
VALUES ('placeholder', '', '', '')
ON CONFLICT DO NOTHING;

-- Bad: Hardcoded secrets
INSERT INTO oauth_tokens (provider, channel, access_token)
VALUES ('twitch', '', 'hardcoded_secret_token');  -- NEVER DO THIS
```

For sensitive data migrations, use separate scripts in `/scripts` that read from environment or secret management system.
