#!/bin/bash
# Seed development data into the vod-tender database
# This script loads sample VODs, chat messages, and configuration data
# for local development and testing purposes.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SEED_SQL="$REPO_ROOT/backend/db/seed-dev-data.sql"

echo -e "${BLUE}=== vod-tender Development Data Seeder ===${NC}"
echo

# Load environment variables if .env exists
if [ -f "$REPO_ROOT/.env" ]; then
    source "$REPO_ROOT/.env"
fi

# Default values
STACK_NAME="${STACK_NAME:-vod}"
DB_NAME="${DB_NAME:-vod}"
DB_USER="${DB_USER:-vod}"
DB_PASSWORD="${DB_PASSWORD:-vod}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5469}"

# Check if running in Docker Compose context
CONTAINER_NAME="${STACK_NAME}-postgres"

# Function to execute SQL
execute_sql() {
    local sql_file=$1
    
    # Try Docker Compose exec first
    if docker compose ps postgres &>/dev/null && [ "$(docker compose ps postgres --format json | jq -r '.[0].State')" = "running" ]; then
        echo -e "${GREEN}✓${NC} Using Docker Compose postgres service"
        docker compose exec -T postgres psql -U "$DB_USER" -d "$DB_NAME" -f /dev/stdin < "$sql_file"
        return $?
    fi
    
    # Try direct container access
    if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
        echo -e "${GREEN}✓${NC} Using container: $CONTAINER_NAME"
        docker exec -i "$CONTAINER_NAME" psql -U "$DB_USER" -d "$DB_NAME" -f /dev/stdin < "$sql_file"
        return $?
    fi
    
    # Try local psql connection
    if command -v psql &>/dev/null; then
        echo -e "${YELLOW}!${NC} Trying local psql connection to $DB_HOST:$DB_PORT"
        PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -f "$sql_file"
        return $?
    fi
    
    echo -e "${RED}✗${NC} Could not connect to database"
    echo "   Please ensure Docker Compose is running (make up)"
    echo "   or PostgreSQL is accessible at $DB_HOST:$DB_PORT"
    return 1
}

# Check if seed SQL file exists
if [ ! -f "$SEED_SQL" ]; then
    echo -e "${RED}✗${NC} Seed SQL file not found: $SEED_SQL"
    exit 1
fi

echo -e "${BLUE}Configuration:${NC}"
echo "  Database: $DB_NAME"
echo "  User: $DB_USER"
echo "  Container: $CONTAINER_NAME"
echo

# Confirm action
if [ "${SEED_CONFIRM:-}" != "yes" ]; then
    echo -e "${YELLOW}This will load sample data into your database.${NC}"
    echo "Existing seed data (records with 'seed-*' IDs) will be replaced."
    echo
    read -p "Continue? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 0
    fi
fi

echo -e "${BLUE}Loading seed data...${NC}"
echo

# Execute the seed SQL
if execute_sql "$SEED_SQL"; then
    echo
    echo -e "${GREEN}✓ Seed data loaded successfully!${NC}"
    echo
    echo -e "${BLUE}What was seeded:${NC}"
    echo "  • 7 sample VODs with various states (completed, downloading, pending, failed)"
    echo "  • 15+ chat messages for the completed VOD (seed-completed-001)"
    echo "  • Circuit breaker configuration (closed state)"
    echo "  • Performance statistics (average download/upload times)"
    echo
    echo -e "${BLUE}Next steps:${NC}"
    echo "  1. Start the services: ${GREEN}make up${NC}"
    echo "  2. View VODs: ${GREEN}curl http://localhost:8080/vods | jq${NC}"
    echo "  3. View status: ${GREEN}curl http://localhost:8080/status | jq${NC}"
    echo "  4. Access frontend: ${GREEN}http://localhost:3000${NC}"
    echo
    echo -e "${BLUE}Useful test data:${NC}"
    echo "  • Completed VOD with chat: seed-completed-001"
    echo "  • In-progress download: seed-downloading-002 (62.5% complete)"
    echo "  • High priority VOD: seed-priority-005"
    echo "  • Failed VOD: seed-failed-004"
    echo
else
    echo
    echo -e "${RED}✗ Failed to load seed data${NC}"
    echo "   Check that the database is running and accessible"
    exit 1
fi
