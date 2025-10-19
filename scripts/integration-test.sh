#!/bin/bash
# Integration test script for vod-tender
# Can be run locally or in CI

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "🧪 Starting integration tests for vod-tender"
echo "Repository root: $REPO_ROOT"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test results
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
pass() {
    echo -e "${GREEN}✓${NC} $1"
    ((TESTS_PASSED++))
}

fail() {
    echo -e "${RED}✗${NC} $1"
    ((TESTS_FAILED++))
}

warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Setup function
setup() {
    echo ""
    echo "📦 Setting up test environment..."
    
    cd "$REPO_ROOT"
    
    # Create network if it doesn't exist
    if ! docker network inspect web >/dev/null 2>&1; then
        echo "Creating docker network 'web'..."
        docker network create web
    fi
    
    # Create .env file
    cat > .env << EOF
STACK_NAME=integration-test
WEB_NETWORK=web
TWITCH_CHANNEL=test-channel
API_PORT=8080
FRONTEND_PORT=8081
DB_NAME=vod
DB_USER=vod
DB_PASSWORD=vod
DB_HOST=postgres
BACKEND_ENV_FILE=./backend/.env
SECRETS_DIR=./secrets
YTDLP_COOKIES_PATH=/run/cookies/twitch-cookies.txt
EOF
    
    # Create backend .env file
    cat > backend/.env << EOF
LOG_LEVEL=info
LOG_FORMAT=text
HTTP_ADDR=:8080
DB_DSN=postgres://vod:vod@postgres:5432/vod?sslmode=disable
DATA_DIR=/data
EOF
    
    # Create secrets directory
    mkdir -p secrets
    
    pass "Environment setup complete"
}

# Start services
start_services() {
    echo ""
    echo "🚀 Starting services with docker-compose..."
    
    cd "$REPO_ROOT"
    docker compose up -d --build
    
    pass "Services started"
}

# Wait for services to be healthy
wait_for_health() {
    echo ""
    echo "⏳ Waiting for services to be healthy..."
    
    # Wait for postgres
    echo "  Waiting for postgres..."
    if timeout 90 bash -c 'until docker compose ps postgres | grep -q "healthy"; do sleep 2; done'; then
        pass "Postgres is healthy"
    else
        fail "Postgres failed to become healthy"
        return 1
    fi
    
    # Wait for API
    echo "  Waiting for API..."
    if timeout 120 bash -c 'until docker compose ps api | grep -q "healthy"; do sleep 2; done'; then
        pass "API is healthy"
    else
        fail "API failed to become healthy"
        docker compose logs api
        return 1
    fi
    
    # Wait for frontend
    echo "  Waiting for frontend..."
    if timeout 120 bash -c 'until docker compose ps frontend | grep -q "healthy"; do sleep 2; done'; then
        pass "Frontend is healthy"
    else
        fail "Frontend failed to become healthy"
        docker compose logs frontend
        return 1
    fi
}

# Run API tests
test_api() {
    echo ""
    echo "🔍 Testing API endpoints..."
    
    # Health endpoint
    if docker compose exec -T api curl -f -s http://localhost:8080/healthz >/dev/null 2>&1; then
        pass "Health endpoint responds"
    else
        fail "Health endpoint failed"
    fi
    
    # Status endpoint
    if docker compose exec -T api curl -f -s http://localhost:8080/status >/dev/null 2>&1; then
        pass "Status endpoint responds"
    else
        fail "Status endpoint failed"
    fi
    
    # Metrics endpoint
    if docker compose exec -T api curl -f -s http://localhost:8080/metrics >/dev/null 2>&1; then
        pass "Metrics endpoint responds"
    else
        fail "Metrics endpoint failed"
    fi
    
    # Check metrics content
    METRICS=$(docker compose exec -T api curl -s http://localhost:8080/metrics)
    if echo "$METRICS" | grep -q "go_goroutines"; then
        pass "Metrics contain Go runtime stats"
    else
        fail "Metrics missing expected content"
    fi
}

# Test frontend
test_frontend() {
    echo ""
    echo "🌐 Testing frontend..."
    
    if docker compose exec -T frontend wget -q -O - http://localhost/ >/dev/null 2>&1; then
        pass "Frontend serves content"
    else
        fail "Frontend failed to serve content"
    fi
    
    # Check if index.html is served
    CONTENT=$(docker compose exec -T frontend wget -q -O - http://localhost/)
    if echo "$CONTENT" | grep -q "<!DOCTYPE html>"; then
        pass "Frontend serves valid HTML"
    else
        fail "Frontend HTML appears invalid"
    fi
}

# Test database
test_database() {
    echo ""
    echo "💾 Testing database..."
    
    # Check if postgres is accessible
    if docker compose exec -T postgres pg_isready -U vod -d vod >/dev/null 2>&1; then
        pass "Database is accessible"
    else
        fail "Database is not accessible"
    fi
    
    # List tables
    TABLES=$(docker compose exec -T postgres psql -U vod -d vod -t -c "\dt" 2>/dev/null || echo "")
    if [ -n "$TABLES" ]; then
        pass "Database has tables"
        echo "     Tables: $(echo "$TABLES" | wc -l)"
    else
        warn "Database appears empty (migrations may not have run)"
    fi
}

# Show service status
show_status() {
    echo ""
    echo "📊 Service Status:"
    docker compose ps
}

# Cleanup
cleanup() {
    echo ""
    echo "🧹 Cleaning up..."
    
    cd "$REPO_ROOT"
    docker compose down -v
    
    # Remove test env files
    rm -f .env backend/.env
    
    pass "Cleanup complete"
}

# Main test flow
main() {
    local EXIT_CODE=0
    
    # Trap cleanup on exit
    trap cleanup EXIT
    
    setup || exit 1
    start_services || exit 1
    wait_for_health || exit 1
    
    test_api
    test_frontend
    test_database
    
    show_status
    
    # Summary
    echo ""
    echo "═══════════════════════════════════════"
    echo "Test Summary"
    echo "═══════════════════════════════════════"
    echo -e "Passed: ${GREEN}${TESTS_PASSED}${NC}"
    echo -e "Failed: ${RED}${TESTS_FAILED}${NC}"
    echo "═══════════════════════════════════════"
    
    if [ $TESTS_FAILED -gt 0 ]; then
        echo ""
        echo "❌ Integration tests FAILED"
        EXIT_CODE=1
    else
        echo ""
        echo "✅ All integration tests PASSED"
        EXIT_CODE=0
    fi
    
    return $EXIT_CODE
}

# Run main
main
exit $?
