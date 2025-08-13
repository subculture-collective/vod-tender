#!/usr/bin/env bash
set -euo pipefail
# Simple Postgres logical backup using pg_dump
# Usage: backup.sh [backup_dir]

BACKUP_DIR=${1:-/backups}
TS=$(date +"%Y%m%d_%H%M%S")
OUT="$BACKUP_DIR/vod_${TS}.sql.gz"

: "${POSTGRES_DB:=vod}"
: "${POSTGRES_USER:=vod}"
: "${POSTGRES_PASSWORD:=vod}"
: "${POSTGRES_HOST:=postgres}"
: "${POSTGRES_PORT:=5432}"

mkdir -p "$BACKUP_DIR"
export PGPASSWORD="$POSTGRES_PASSWORD"
pg_dump -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --no-privileges | gzip -c > "$OUT"
echo "backup written: $OUT"
