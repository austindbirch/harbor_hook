#!/bin/bash
# demo.sh - Demonstration script for harborctl with JWT authentication

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print colored output
print_step() {
    echo -e "${BLUE}==> $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Configuration
TENANT_ID=${TENANT_ID:-"demo_tenant"}
WEBHOOK_URL="http://localhost:8081/hook"
EVENT_TYPE="demo.event"
SERVER_HOST=${SERVER_HOST:-"localhost:8443"}  # HTTPS gateway
JWKS_HOST=${JWKS_HOST:-"localhost:8082"}      # JWT token server

echo "🚀 Harborhook CLI Demo (with JWT Authentication)"
echo "=================================================="

# Function to get JWT token
get_jwt_token() {
    local token_response
    token_response=$(curl -s -X POST "http://$JWKS_HOST/token" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\"}" || {
        print_error "Failed to get JWT token from $JWKS_HOST"
        exit 1
    })
    
    # Extract token from JSON response
    echo "$token_response" | jq -r '.token' 2>/dev/null || {
        print_error "Failed to parse JWT token from response"
        echo "Response: $token_response"
        exit 1
    }
}

# Check if harborctl is available
if [ ! -f "./bin/harborctl" ]; then
    echo "❌ harborctl not found at ./bin/harborctl. Please build it first:"
    echo "   make build-cli"
    exit 1
fi

# Use local binary
HARBORCTL="./bin/harborctl"

# Get JWT token
print_step "Obtaining JWT token"
JWT_TOKEN=$(get_jwt_token)
if [[ -z "$JWT_TOKEN" || "$JWT_TOKEN" == "null" ]]; then
    print_error "Failed to obtain valid JWT token"
    exit 1
fi
print_success "JWT token obtained successfully"

# Export token for harborctl to use
export JWT_TOKEN

print_step "Checking service health..."
$HARBORCTL ping --server "$SERVER_HOST"

print_step "Creating webhook endpoint..."
ENDPOINT_RESP=$($HARBORCTL endpoint create "$TENANT_ID" "$WEBHOOK_URL" --server "$SERVER_HOST" --json)
ENDPOINT_ID=$(echo "$ENDPOINT_RESP" | jq -r '.endpoint.id')
print_success "Created endpoint: $ENDPOINT_ID"

print_step "Creating subscription..."
SUBSCRIPTION_RESP=$($HARBORCTL subscription create "$TENANT_ID" "$ENDPOINT_ID" "$EVENT_TYPE" --server "$SERVER_HOST" --json)
SUBSCRIPTION_ID=$(echo "$SUBSCRIPTION_RESP" | jq -r '.subscription.id')
print_success "Created subscription: $SUBSCRIPTION_ID"

print_step "Publishing test event..."
PAYLOAD='{"demo": true, "message": "Hello from harborctl!", "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}'
EVENT_RESP=$($HARBORCTL event publish "$TENANT_ID" "$EVENT_TYPE" "$PAYLOAD" --server "$SERVER_HOST" --json)
EVENT_ID=$(echo "$EVENT_RESP" | jq -r '.eventId')
FANOUT_COUNT=$(echo "$EVENT_RESP" | jq -r '.fanoutCount')
print_success "Published event: $EVENT_ID (fanout: $FANOUT_COUNT)"

print_step "Waiting for delivery..."
sleep 3

print_step "Checking delivery status..."
$HARBORCTL delivery status "$EVENT_ID" --server "$SERVER_HOST"

print_step "Listing recent DLQ entries..."
$HARBORCTL delivery dlq --limit 5 --server "$SERVER_HOST"

print_success "Demo completed successfully!"
echo -e "\n${GREEN}All operations completed with JWT authentication via HTTPS gateway${NC}"

echo -e "\n✅ Demo completed successfully!"
echo "   Event ID: $EVENT_ID"
echo "   Endpoint ID: $ENDPOINT_ID"
echo "   Subscription ID: $SUBSCRIPTION_ID"
