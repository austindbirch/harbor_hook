#!/bin/bash
# test_jwt_auth.sh - Quick test of JWT authentication

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_step() {
    echo -e "${BLUE}==> $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Configuration
SERVER_HOST=${SERVER_HOST:-"localhost:8443"}
JWKS_HOST=${JWKS_HOST:-"localhost:8082"}
TENANT_ID=${TENANT_ID:-"demo_tenant"}
HARBORCTL="./bin/harborctl"

echo "🔐 JWT Authentication Test"
echo "=========================="

# Check if harborctl is available
if [ ! -f "$HARBORCTL" ]; then
    print_error "harborctl not found at $HARBORCTL. Please build it first:"
    echo "   make build-cli"
    exit 1
fi

# Get JWT token
print_step "Getting JWT token from $JWKS_HOST"
TOKEN_RESPONSE=$(curl -s -X POST "http://$JWKS_HOST/token" \
    -H "Content-Type: application/json" \
    -d "{\"tenant_id\":\"$TENANT_ID\"}" || {
    print_error "Failed to get JWT token"
    exit 1
})

JWT_TOKEN=$(echo "$TOKEN_RESPONSE" | jq -r '.token' 2>/dev/null || {
    print_error "Failed to parse JWT token"
    echo "Response: $TOKEN_RESPONSE"
    exit 1
})

if [[ -z "$JWT_TOKEN" || "$JWT_TOKEN" == "null" ]]; then
    print_error "Failed to obtain valid JWT token"
    exit 1
fi

print_success "JWT token obtained"

# Test without token (should fail)
print_step "Testing ping without token (should fail)"
if $HARBORCTL ping --server "$SERVER_HOST" >/dev/null 2>&1; then
    print_error "Ping succeeded without token - authentication not working!"
else
    print_success "Ping failed without token - authentication is working"
fi

# Test with token (should succeed)
print_step "Testing ping with JWT token"
export JWT_TOKEN
if $HARBORCTL ping --server "$SERVER_HOST" >/dev/null 2>&1; then
    print_success "Ping succeeded with JWT token - authentication working!"
else
    print_error "Ping failed with JWT token - check configuration"
    exit 1
fi

print_success "JWT authentication test completed successfully!"
