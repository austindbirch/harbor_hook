#!/usr/bin/env bash
set -Eeuo pipefail

# --- Config (override via env before running if you like) ---
TENANT="${TENANT:-demo_tenant}"
EVENT_TYPE="${EVENT_TYPE:-appointment.created}"
RECEIVER_URL="${RECEIVER_URL:-http://fake-receiver:8081/hook}"  # should return 5xx to exercise retries
SECRET="${SECRET:-demo_secret}"
COUNT="${COUNT:-3}"
SERVER_HOST="${SERVER_HOST:-localhost:8443}"  # HTTPS gateway
JWKS_HOST="${JWKS_HOST:-localhost:8082}"      # JWT token server
HARBORCTL="${HARBORCTL:-./bin/harborctl}"

# Function to get JWT token
get_jwt_token() {
    local token_response
    token_response=$(curl -s -X POST "http://$JWKS_HOST/token" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT\"}" || {
        echo "Failed to get JWT token from $JWKS_HOST" >&2
        exit 1
    })
    
    # Extract token from JSON response
    echo "$token_response" | jq -r '.token' 2>/dev/null || {
        echo "Failed to parse JWT token from response" >&2
        echo "Response: $token_response" >&2
        exit 1
    }
}

# Get JWT token
echo "→ Obtaining JWT token"
JWT_TOKEN=$(get_jwt_token)
if [[ -z "$JWT_TOKEN" || "$JWT_TOKEN" == "null" ]]; then
    echo "Failed to obtain valid JWT token" >&2
    exit 1
fi
export JWT_TOKEN
echo "✓ JWT token obtained successfully"

echo "→ Creating endpoint for tenant '$TENANT' → $RECEIVER_URL"
EP_JSON=$($HARBORCTL endpoint create "$TENANT" "$RECEIVER_URL" --secret "$SECRET" --server "$SERVER_HOST" --json)
EP_ID=$(echo "$EP_JSON" | jq -r '.endpoint.id')
echo "✓ Endpoint ID: $EP_ID"

echo "→ Subscribing endpoint to '$EVENT_TYPE'"
SUB_JSON=$($HARBORCTL subscription create "$TENANT" "$EP_ID" "$EVENT_TYPE" --server "$SERVER_HOST" --json)
echo "  subscription response: $SUB_JSON"

echo "→ Publishing $COUNT event(s) (unique idempotency keys)"
for i in $(seq 1 "$COUNT"); do
  IDEM="idem-backoff-$(date +%s)-$RANDOM-$i"
  LABEL="evt-backoff-$i"
  PAYLOAD="{\"id\":\"$LABEL\"}"

  PUB_JSON=$($HARBORCTL event publish "$TENANT" "$EVENT_TYPE" "$PAYLOAD" --idempotency-key "$IDEM" --server "$SERVER_HOST" --json)
  EVENT_ID=$(echo "$PUB_JSON" | jq -r '.eventId // .event_id // empty')
  FANOUT=$(echo "$PUB_JSON" | jq -r '.fanoutCount // .fanout_count // empty')

  echo "  • Publish #$i  idempotency_key=$IDEM  label=$LABEL"
  echo "    → response: $PUB_JSON"
  echo "    → eventId: ${EVENT_ID:-<none>}  fanoutCount: ${FANOUT:-<n/a>}"
done

cat <<'TIP'

Next:
  • Open nsqadmin (http://localhost:4171) → Topics → deliveries → Channel workers
    - You should see Requeued increase and Deferred > 0 during delays.
  • If you don't see retries, ensure your worker uses DisableAutoResponse() and calls Requeue(delay) on failures.

TIP