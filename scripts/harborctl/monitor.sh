#!/bin/bash
# monitor.sh - Real-time monitoring for Harborhook deliveries with JWT authentication

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Configuration
REFRESH_INTERVAL=${REFRESH_INTERVAL:-5}
SERVER_HOST=${SERVER_HOST:-"localhost:8443"}  # HTTPS gateway
JWKS_HOST=${JWKS_HOST:-"localhost:8082"}      # JWT token server
TENANT_ID=${TENANT_ID:-"demo_tenant"}
HARBORCTL="${HARBORCTL:-./bin/harborctl}"

# Print colored output
print_header() {
    echo -e "${BOLD}${BLUE}$1${NC}"
}

print_success() {
    echo -e "${GREEN}$1${NC}"
}

print_warning() {
    echo -e "${YELLOW}$1${NC}"
}

print_error() {
    echo -e "${RED}$1${NC}"
}

# Function to get JWT token
get_jwt_token() {
    local token_response
    token_response=$(curl -s -X POST "http://$JWKS_HOST/token" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\"}" 2>/dev/null || {
        print_error "Failed to get JWT token from $JWKS_HOST"
        return 1
    })
    
    # Extract token from JSON response
    echo "$token_response" | jq -r '.token' 2>/dev/null || {
        print_error "Failed to parse JWT token from response"
        return 1
    }
}

# Function to refresh JWT token
refresh_token() {
    JWT_TOKEN=$(get_jwt_token)
    if [[ -z "$JWT_TOKEN" || "$JWT_TOKEN" == "null" ]]; then
        print_error "Failed to refresh JWT token"
        return 1
    fi
    export JWT_TOKEN
}

# Cleanup function
cleanup() {
    echo -e "
${YELLOW}Monitoring stopped${NC}"
    exit 0
}

# Set up signal handling
trap cleanup INT TERM

print_header "🔍 Harborhook Delivery Monitor (with JWT Authentication)"
echo "=========================================================="
echo "Refresh interval: ${REFRESH_INTERVAL}s"
echo "Server: $SERVER_HOST (HTTPS)"
echo "JWKS server: $JWKS_HOST"
echo "Tenant: $TENANT_ID"
echo "Press Ctrl+C to stop"
echo

# Initial token
refresh_token || {
    print_error "Failed to obtain initial JWT token"
    exit 1
}

# Token refresh counter (refresh every 50 iterations to avoid token expiry)
token_refresh_counter=0

while true; do
    # Refresh token periodically
    if [ $((token_refresh_counter % 50)) -eq 0 ]; then
        refresh_token || {
            print_warning "Token refresh failed, continuing with existing token"
        }
    fi
    token_refresh_counter=$((token_refresh_counter + 1))

    # Clear screen and show timestamp
    clear
    print_header "🔍 Harborhook Delivery Monitor"
    echo "Last updated: $(date)"
    echo "Token status: $([ -n "$JWT_TOKEN" ] && echo "✓ Valid" || echo "✗ Invalid")"
    echo "================================================"
    
    # Health check
    echo -e "
${BOLD}Service Health:${NC}"
    if $HARBORCTL ping --server "$SERVER_HOST" >/dev/null 2>&1; then
        print_success "✓ Service is healthy"
    else
        print_error "✗ Service unavailable"
    fi
    
    # Recent delivery stats (if available)
    echo -e "
${BOLD}Recent Delivery Activity:${NC}"
    
    # Show recent DLQ entries
    echo -e "
${BOLD}Dead Letter Queue (Last 10):${NC}"
    DLQ_OUTPUT=$($HARBORCTL delivery dlq --limit 10 --server "$SERVER_HOST" 2>/dev/null || echo "Failed to fetch DLQ")
    
    if [[ "$DLQ_OUTPUT" == "Failed to fetch DLQ" ]]; then
        print_warning "Unable to fetch DLQ entries"
    elif [[ -z "$DLQ_OUTPUT" || "$DLQ_OUTPUT" == *"No DLQ entries found"* ]]; then
        print_success "✓ No failed deliveries"
    else
        echo "$DLQ_OUTPUT"
    fi
    
    echo -e "
${YELLOW}Refreshing in ${REFRESH_INTERVAL}s... (Ctrl+C to stop)${NC}"
    sleep $REFRESH_INTERVAL
done
