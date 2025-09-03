#!/bin/bash
# demo.sh - Demonstration script for harborctl

set -e

TENANT_ID="demo_tenant"
WEBHOOK_URL="http://fake-receiver:8081/hook"
EVENT_TYPE="demo.event"

echo "🚀 Harbor Hook CLI Demo"
echo "======================"

# Check if harborctl is available
if ! command -v harborctl &> /dev/null; then
    echo "❌ harborctl not found. Please build and install it first:"
    echo "   make build-cli && make install-cli"
    exit 1
fi

echo "1. Checking service health..."
harborctl ping

echo -e "\n2. Creating webhook endpoint..."
ENDPOINT_RESP=$(harborctl endpoint create "$TENANT_ID" "$WEBHOOK_URL" --json)
ENDPOINT_ID=$(echo "$ENDPOINT_RESP" | jq -r '.endpoint.id')
echo "   Created endpoint: $ENDPOINT_ID"

echo -e "\n3. Creating subscription..."
SUBSCRIPTION_RESP=$(harborctl subscription create "$TENANT_ID" "$ENDPOINT_ID" "$EVENT_TYPE" --json)
SUBSCRIPTION_ID=$(echo "$SUBSCRIPTION_RESP" | jq -r '.subscription.id')
echo "   Created subscription: $SUBSCRIPTION_ID"

echo -e "\n4. Publishing test event..."
PAYLOAD='{"demo": true, "message": "Hello from harborctl!", "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}'
EVENT_RESP=$(harborctl event publish "$TENANT_ID" "$EVENT_TYPE" "$PAYLOAD" --json)
EVENT_ID=$(echo "$EVENT_RESP" | jq -r '.eventId')
FANOUT_COUNT=$(echo "$EVENT_RESP" | jq -r '.fanoutCount')
echo "   Published event: $EVENT_ID (fanout: $FANOUT_COUNT)"

echo -e "\n5. Waiting for delivery..."
sleep 3

echo -e "\n6. Checking delivery status..."
harborctl delivery status "$EVENT_ID"

echo -e "\n7. Listing recent DLQ entries..."
harborctl delivery dlq --limit 5

echo -e "\n✅ Demo completed successfully!"
echo "   Event ID: $EVENT_ID"
echo "   Endpoint ID: $ENDPOINT_ID"
echo "   Subscription ID: $SUBSCRIPTION_ID"
