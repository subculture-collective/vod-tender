#!/usr/bin/env bash
# Monthly database restore drill script
# Purpose: Validate backup integrity and practice recovery procedures
# Run this monthly (first Monday of each month) to ensure backups are restorable
#
# Usage:
#   ./scripts/test-restore.sh
#
# Environment variables:
#   AWS_ACCESS_KEY_ID - AWS credentials for S3 access
#   AWS_SECRET_ACCESS_KEY - AWS credentials for S3 access
#   S3_BUCKET - S3 bucket name (default: vod-tender-backups)
#   POSTGRES_HOST - PostgreSQL host (default: localhost)
#   POSTGRES_USER - PostgreSQL superuser (default: postgres)
#   PGPASSWORD - PostgreSQL password (required for authentication)
#                Alternative: configure ~/.pgpass file
#
# Note: Ensure PostgreSQL authentication is configured via PGPASSWORD
#       environment variable or ~/.pgpass file before running this script.

set -euo pipefail

# Configuration
S3_BUCKET="${S3_BUCKET:-vod-tender-backups}"
S3_PREFIX="${S3_PREFIX:-database/}"
POSTGRES_HOST="${POSTGRES_HOST:-localhost}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
TEST_DB_NAME="vod_restore_test"
RESTORE_LOG="/var/log/vod-tender/restore-drill-$(date +%Y%m%d).log"

# Ensure log directory exists
mkdir -p "$(dirname "$RESTORE_LOG")"

# Cleanup function for trap handler
cleanup_on_exit() {
  local exit_code=$?
  if [ -n "${TEST_DB_NAME:-}" ]; then
    log "Cleaning up test database..."
    psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -c "DROP DATABASE IF EXISTS ${TEST_DB_NAME};" 2>/dev/null || true
  fi
  if [ -n "${BACKUP_FILE:-}" ] && [ -f "$BACKUP_FILE" ]; then
    log "Removing backup file..."
    rm -f "$BACKUP_FILE"
  fi
  exit "$exit_code"
}

# Register cleanup handler
trap cleanup_on_exit EXIT ERR

# Logging functions
log() {
  echo "$@" | tee -a "$RESTORE_LOG"
}

log_step() {
  log ""
  log "[$1] $2"
}

# Start drill
log "=== Database Restore Drill Started ==="
log "Date: $(date)"
log "S3 Bucket: s3://${S3_BUCKET}/${S3_PREFIX}"
log "PostgreSQL: ${POSTGRES_USER}@${POSTGRES_HOST}"

# Step 1: Download latest backup from S3
log_step "1/6" "Downloading latest backup from S3..."
LATEST_BACKUP=$(aws s3 ls "s3://${S3_BUCKET}/${S3_PREFIX}" | sort | tail -1 | awk '{print $4}')
if [ -z "$LATEST_BACKUP" ]; then
  log "❌ FAILED: No backups found in S3"
  exit 1
fi
log "Latest backup: $LATEST_BACKUP"

BACKUP_FILE="/tmp/${LATEST_BACKUP}"
set +e
aws s3 cp "s3://${S3_BUCKET}/${S3_PREFIX}${LATEST_BACKUP}" "$BACKUP_FILE" 2>&1 | tee -a "$RESTORE_LOG"
AWS_EXIT_CODE=${PIPESTATUS[0]}
set -e
if [ "$AWS_EXIT_CODE" -ne 0 ]; then
  log "❌ FAILED: aws s3 cp failed with exit code $AWS_EXIT_CODE"
  exit 1
fi
BACKUP_SIZE=$(du -h "$BACKUP_FILE" | cut -f1)
log "Downloaded: $BACKUP_FILE (${BACKUP_SIZE})"

# Step 2: Create test database
log_step "2/6" "Creating test database..."
set +e
psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -c "DROP DATABASE IF EXISTS ${TEST_DB_NAME};" 2>&1 | tee -a "$RESTORE_LOG"
PSQL_EXIT_CODE=${PIPESTATUS[0]}
set -e
if [ "$PSQL_EXIT_CODE" -ne 0 ]; then
  log "❌ FAILED: DROP DATABASE failed with exit code $PSQL_EXIT_CODE"
  exit 1
fi

set +e
psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -c "CREATE DATABASE ${TEST_DB_NAME};" 2>&1 | tee -a "$RESTORE_LOG"
PSQL_EXIT_CODE=${PIPESTATUS[0]}
set -e
if [ "$PSQL_EXIT_CODE" -ne 0 ]; then
  log "❌ FAILED: CREATE DATABASE failed with exit code $PSQL_EXIT_CODE"
  exit 1
fi

# Step 3: Restore backup
log_step "3/6" "Restoring backup to test database..."
START_TIME=$(date +%s)
set +e
(
  set -o pipefail
  zcat "$BACKUP_FILE" | psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" 2>&1 | tee -a "$RESTORE_LOG"
)
RESTORE_STATUS=("${PIPESTATUS[@]}")
set -e
if [ "${RESTORE_STATUS[0]}" -ne 0 ]; then
  log "❌ FAILED: zcat failed with exit code ${RESTORE_STATUS[0]}"
  exit 1
fi
if [ "${RESTORE_STATUS[1]}" -ne 0 ]; then
  log "❌ FAILED: psql restore failed with exit code ${RESTORE_STATUS[1]}"
  exit 1
fi
END_TIME=$(date +%s)
RESTORE_DURATION=$((END_TIME - START_TIME))
log "Restore completed in ${RESTORE_DURATION} seconds"

# Step 4: Verify data integrity
log_step "4/6" "Verifying data integrity..."

# 4.1: Check table existence
log "Checking table structure..."
TABLES=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -c "
  SELECT table_name FROM information_schema.tables 
  WHERE table_schema='public' ORDER BY table_name;
")
TABLE_COUNT=$(echo "$TABLES" | wc -l)
log "Tables found: $TABLE_COUNT"
echo "$TABLES" | tee -a "$RESTORE_LOG"

# 4.2: Verify row counts
log "Checking row counts..."
VODS_COUNT=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -c "SELECT COUNT(*) FROM vods;" 2>/dev/null || echo "0")
VODS_COUNT=${VODS_COUNT:-0}
CHAT_COUNT=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -c "SELECT COUNT(*) FROM chat_messages;" 2>/dev/null || echo "0")
CHAT_COUNT=${CHAT_COUNT:-0}
OAUTH_COUNT=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -c "SELECT COUNT(*) FROM oauth_tokens;" 2>/dev/null || echo "0")
OAUTH_COUNT=${OAUTH_COUNT:-0}

log "Row counts:"
log "  vods: ${VODS_COUNT}"
log "  chat_messages: ${CHAT_COUNT}"
log "  oauth_tokens: ${OAUTH_COUNT}"

# 4.3: Verify indexes
log "Checking indexes..."
INDEX_COUNT=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -c "
  SELECT COUNT(*) FROM pg_indexes WHERE schemaname='public';
")
log "Indexes: ${INDEX_COUNT}"

# 4.4: Check for processed VODs
PROCESSED_VODS=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -c "
  SELECT COUNT(*) FROM vods WHERE processed=true;
")
log "Processed VODs: ${PROCESSED_VODS}"

# 4.5: Verify most recent VOD
log "Checking most recent VOD..."
LATEST_VOD=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -F'|' -c "
  SELECT twitch_vod_id, title, created_at 
  FROM vods 
  ORDER BY created_at DESC 
  LIMIT 1;
")
log "Latest VOD: ${LATEST_VOD}"

# Step 5: Validate referential integrity
log_step "5/6" "Validating referential integrity..."
ORPHANED_CHATS=$(psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" "$TEST_DB_NAME" -At -c "
  SELECT COUNT(*) FROM chat_messages cm 
  WHERE NOT EXISTS (SELECT 1 FROM vods v WHERE v.twitch_vod_id = cm.vod_id);
")
log "Orphaned chat messages: ${ORPHANED_CHATS}"

# Step 6: Cleanup
log_step "6/6" "Cleaning up..."
# Disable trap handler before manual cleanup
trap - EXIT ERR
psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -c "DROP DATABASE ${TEST_DB_NAME};" 2>&1 | tee -a "$RESTORE_LOG"
rm "$BACKUP_FILE"

# Final result
log ""
log "=== Restore Drill Summary ==="
if [ "$VODS_COUNT" -gt 0 ] && [ "$ORPHANED_CHATS" -eq 0 ]; then
  log "✅ PASSED: Restore drill completed successfully"
  log "   - Backup size: ${BACKUP_SIZE}"
  log "   - Restore time: ${RESTORE_DURATION}s"
  log "   - VODs restored: ${VODS_COUNT}"
  log "   - Chat messages: ${CHAT_COUNT}"
  log "   - Data integrity: ✓"
  log ""
  log "Next steps:"
  log "  1. Document this drill in the tracking log (see RUNBOOKS.md)"
  log "  2. Review any warnings or errors above"
  log "  3. Schedule next drill for first Monday of next month"
  exit 0
else
  log "❌ FAILED: Restore drill encountered issues"
  log "   - VODs count: ${VODS_COUNT}"
  log "   - Orphaned chats: ${ORPHANED_CHATS}"
  log ""
  log "Action required:"
  log "  1. Investigate why VODs count is zero or orphaned chats exist"
  log "  2. Verify backup process is working correctly"
  log "  3. Check logs for errors during restore"
  exit 1
fi
