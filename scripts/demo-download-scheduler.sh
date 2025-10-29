#!/bin/bash
# Demo script for download scheduler features
# Run this against a running vod-tender instance

set -e

API_BASE="${API_BASE:-http://localhost:8080}"
ADMIN_AUTH="${ADMIN_AUTH:-}"

echo "=== Download Scheduler Demo ==="
echo "API Base: $API_BASE"
echo

# Helper function for API calls
api_call() {
    local method=$1
    local endpoint=$2
    local data=${3:-}
    
    local auth_header=""
    if [ -n "$ADMIN_AUTH" ]; then
        auth_header="-H X-Admin-Token: $ADMIN_AUTH"
    fi
    
    if [ -n "$data" ]; then
        curl -s -X "$method" "$API_BASE$endpoint" \
            $auth_header \
            -H "Content-Type: application/json" \
            -d "$data"
    else
        curl -s -X "$method" "$API_BASE$endpoint" $auth_header
    fi
}

# 1. Check initial status
echo "1. Checking current status..."
status=$(api_call GET /status)
echo "$status" | jq '{
    pending: .pending,
    active_downloads: .active_downloads,
    max_concurrent: .max_concurrent_downloads,
    queue_by_priority: .queue_by_priority
}'
echo

# 2. Get first unprocessed VOD
echo "2. Finding first unprocessed VOD..."
vods=$(api_call GET "/vods?limit=1")
first_vod=$(echo "$vods" | jq -r '.[0].id // empty')

if [ -z "$first_vod" ]; then
    echo "No unprocessed VODs found. Exiting demo."
    exit 0
fi

echo "Found VOD: $first_vod"
echo

# 3. Bump priority
echo "3. Bumping VOD priority to 100..."
priority_result=$(api_call POST /admin/vod/priority "{\"vod_id\":\"$first_vod\",\"priority\":100}")
echo "$priority_result" | jq '.'
echo

# 4. Check updated status
echo "4. Checking updated queue status..."
updated_status=$(api_call GET /status)
echo "$updated_status" | jq '.queue_by_priority'
echo

# 5. Show retry configuration
echo "5. Current retry configuration..."
echo "$updated_status" | jq '.retry_config'
echo

# 6. Check bandwidth limit
echo "6. Bandwidth limit configuration..."
bandwidth_limit=$(echo "$updated_status" | jq -r '.download_rate_limit // "Not configured"')
echo "Download rate limit: $bandwidth_limit"
echo

# 7. Monitor for a few seconds
echo "7. Monitoring active downloads (5 seconds)..."
for i in {1..5}; do
    current_status=$(api_call GET /status)
    active=$(echo "$current_status" | jq -r '.active_downloads')
    pending=$(echo "$current_status" | jq -r '.pending')
    echo "  [$i/5] Active: $active, Pending: $pending"
    sleep 1
done
echo

# 8. Reset priority
echo "8. Resetting VOD priority to default (0)..."
reset_result=$(api_call POST /admin/vod/priority "{\"vod_id\":\"$first_vod\",\"priority\":0}")
echo "$reset_result" | jq '.'
echo

echo "=== Demo Complete ==="
echo
echo "Try these commands:"
echo "  # Check status"
echo "  curl $API_BASE/status | jq"
echo
echo "  # Set priority"
echo "  curl -X POST $API_BASE/admin/vod/priority \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"vod_id\":\"YOUR_VOD_ID\",\"priority\":100}'"
echo
echo "  # Monitor active downloads"
echo "  watch -n 1 'curl -s $API_BASE/status | jq \"{active: .active_downloads, pending: .pending}\"'"
