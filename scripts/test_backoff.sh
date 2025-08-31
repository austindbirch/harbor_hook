#!/usr/bin/env bash
set -Eeuo pipefail

# --- Config (override via env before running if you like) ---
API_BASE="${API_BASE:-http://localhost:8080}"
TENANT="${TENANT:-tn_123}"
EVENT_TYPE="${EVENT_TYPE:-appointment.created}"
RECEIVER_URL="${RECEIVER_URL:-http://fake-receiver:8081/hook}"  # should return 5xx to exercise retries
SECRET="${SECRET:-demo_secret}"
COUNT="${COUNT:-3}"

post_json() {
  local url="$1"; shift
  curl -sS -X POST "$url" -H 'Content-Type: application/json' -d "$@"
}

echo "→ Creating endpoint for tenant '$TENANT' → $RECEIVER_URL"
EP_JSON="$(post_json "$API_BASE/v1/tenants/$TENANT/endpoints" \
  "$(jq -n --arg url "$RECEIVER_URL" --arg secret "$SECRET" '{url:$url,secret:$secret}')" )"
EP_ID="$(printf '%s' "$EP_JSON" | jq -r '.endpoint.id')"
echo "✓ Endpoint ID: $EP_ID"

echo "→ Subscribing endpoint to '$EVENT_TYPE'"
SUB_RES="$(post_json "$API_BASE/v1/tenants/$TENANT/subscriptions" \
  "$(jq -n --arg et "$EVENT_TYPE" --arg ep "$EP_ID" '{event_type:$et, endpoint_id:$ep}')" )"
echo "  subscription response: $SUB_RES"

echo "→ Publishing $COUNT event(s) (unique idempotency keys)"
for i in $(seq 1 "$COUNT"); do
  IDEM="idem-backoff-$(date +%s)-$RANDOM-$i"
  LABEL="evt-backoff-$i"

  PAYLOAD="$(jq -n \
    --arg et "$EVENT_TYPE" \
    --arg id "$LABEL" \
    --arg idem "$IDEM" \
    '{event_type:$et, payload:{id:$id}, idempotency_key:$idem}')"

  PUB_JSON="$(post_json "$API_BASE/v1/tenants/$TENANT/events:publish" "$PAYLOAD")"
  EVENT_ID="$(printf '%s' "$PUB_JSON" | jq -r '.eventId // .event_id // empty')"
  FANOUT="$(printf '%s' "$PUB_JSON" | jq -r '.fanoutCount // .fanout_count // empty')"

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