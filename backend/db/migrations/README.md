# Database Migrations

This directory contains versioned database migrations for vod-tender using [golang-migrate](https://github.com/golang-migrate/migrate).

## Migration Files

Migrations follow a naming convention:
```
{version}_{description}.up.sql    - Applies the migration (forward)
{version}_{description}.down.sql  - Reverts the migration (rollback)
```

Examples:
- `000001_initial_schema.up.sql` / `000001_initial_schema.down.sql`
- `000002_add_performance_indices.up.sql` / `000002_add_performance_indices.down.sql`

## Running Migrations

### In Application

Migrations run automatically when the application starts via `db.RunMigrations()` in `main.go`.

The application will:
1. Try to run versioned migrations from this directory
2. Fall back to legacy `db.Migrate()` if versioned migrations fail
3. Log migration status and version

### Using Makefile (Development)

```bash
# Install golang-migrate CLI tool
make migrate-install

# Create a new migration
make migrate-create name=add_something

# Apply all pending migrations
make migrate-up

# Rollback the last migration
make migrate-down

# Show current migration version
make migrate-status

# Force set version (DANGEROUS - use only to fix dirty state)
make migrate-force VERSION=2
```

### Using migrate CLI Directly

```bash
# Install
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Set DSN
# For Docker Compose stack (host port 5469 maps to container port 5432):
export DSN="postgres://vod:vod@localhost:5469/vod?sslmode=disable"
# For direct connection or different port:
# export DSN="postgres://vod:vod@localhost:5432/vod?sslmode=disable"

# Create migration
migrate create -ext sql -dir backend/db/migrations -seq add_something

# Run migrations
migrate -path backend/db/migrations -database "$DSN" up

# Rollback
migrate -path backend/db/migrations -database "$DSN" down 1

# Check version
migrate -path backend/db/migrations -database "$DSN" version
```

## Writing Migrations

### Best Practices

1. **Always reversible**: Every `up` migration must have a working `down` migration
2. **Idempotent**: Use `IF NOT EXISTS` / `IF EXISTS` where possible
3. **Transactional**: Wrap DDL in `BEGIN`/`COMMIT` for atomicity
4. **Small steps**: One logical change per migration
5. **Tested**: Test both up and down migrations before committing

### Creating Indices

For regular CREATE INDEX (works in transactions):
```sql
BEGIN;
CREATE INDEX IF NOT EXISTS idx_name ON table(column);
COMMIT;
```

For zero-downtime index creation in production (cannot use transactions):
```sql
-- Note: Run manually in production with CONCURRENTLY
-- This migration uses regular CREATE INDEX for compatibility
CREATE INDEX IF NOT EXISTS idx_name ON table(column);
```

### Example Migration

**000003_add_user_column.up.sql:**
```sql
BEGIN;

ALTER TABLE vods ADD COLUMN IF NOT EXISTS user_id TEXT;
CREATE INDEX IF NOT EXISTS idx_vods_user_id ON vods(user_id);

COMMIT;
```

**000003_add_user_column.down.sql:**
```sql
BEGIN;

DROP INDEX IF EXISTS idx_vods_user_id;
ALTER TABLE vods DROP COLUMN IF EXISTS user_id;

COMMIT;
```

## Migration History

Migrations are tracked in the `schema_migrations` table:
```sql
SELECT * FROM schema_migrations;
```

Columns:
- `version`: Current migration version number
- `dirty`: Whether a migration failed (requires manual intervention)

## Troubleshooting

### Dirty State

If a migration fails, the database is marked as "dirty":

```bash
# Check version and dirty state
make migrate-status

# Fix the issue, then force the version
make migrate-force VERSION=X

# Try migrating again
make migrate-up
```

### Path Issues

Migrations are located automatically using `getMigrationsPath()` which searches:
- `db/migrations` (when running from backend/)
- `migrations` (when running from backend/db/)
- `backend/db/migrations` (when running from repo root)

### Legacy Migration Compatibility

The old `db.Migrate()` function is still available as a fallback. New deployments should use `db.RunMigrations()`.

## Testing

Run migration tests:
```bash
# Requires TEST_PG_DSN environment variable
# Port 5433 avoids conflict with main docker-compose Postgres (5469)
export TEST_PG_DSN="postgres://vod:vod@localhost:5433/vod_test?sslmode=disable"

# Start test Postgres container
docker run -d --name test-postgres \
  -e POSTGRES_USER=vod \
  -e POSTGRES_PASSWORD=vod \
  -e POSTGRES_DB=vod_test \
  -p 5433:5432 \
  postgres:16

# Run tests
go test -v ./db -run TestMigration

# Cleanup
docker stop test-postgres && docker rm test-postgres
```

Tests verify:
- Migrations apply cleanly on empty database
- Migrations are idempotent (can run multiple times)
- Forward and backward migration works
- Data is preserved during migrations
- Rollback restores previous state
