#!/bin/bash

# Generate JWT tokens for testing

JWKS_URL="${JWKS_URL:-http://localhost:8082}"

if [ $# -eq 0 ]; then
    echo "Usage: $0 <tenant_id> [ttl_seconds]"
    echo "Example: $0 tn_123 3600"
    exit 1
fi

TENANT_ID="$1"
TTL="${2:-3600}"

echo "Creating JWT token for tenant: ${TENANT_ID}"
echo "TTL: ${TTL} seconds"
echo

TOKEN_RESPONSE=$(curl -s "${JWKS_URL}/token" \
    -H "Content-Type: application/json" \
    -d "{\"tenant_id\": \"${TENANT_ID}\", \"ttl_seconds\": ${TTL}}")

if [ $? -ne 0 ]; then
    echo "❌ Failed to connect to JWKS server at ${JWKS_URL}"
    exit 1
fi

TOKEN=$(echo "${TOKEN_RESPONSE}" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

if [ -z "${TOKEN}" ]; then
    echo "❌ Failed to get JWT token"
    echo "Response: ${TOKEN_RESPONSE}"
    exit 1
fi

echo "✅ JWT token created successfully"
echo
echo "Token: ${TOKEN}"
echo
echo "Usage examples:"
echo "# Make authenticated request:"
echo "curl -k -H \"Authorization: Bearer ${TOKEN}\" https://localhost:8443/v1/ping"
echo
echo "# Create endpoint:"
echo "curl -k -H \"Authorization: Bearer ${TOKEN}\" -H \"Content-Type: application/json\" \\"
echo "  https://localhost:8443/v1/tenants/${TENANT_ID}/endpoints \\"
echo "  -d '{\"url\": \"https://example.com/webhook\", \"secret\": \"test123\"}'"
echo
echo "# Decode token (requires jwt-cli or similar):"
echo "echo '${TOKEN}' | jwt decode -"
