#!/usr/bin/env bash
set -Eeuo pipefail

# --- Config (override via env) ---
RECEIVER_URL="${RECEIVER_URL:-http://localhost:8081/hook}"  # or http://fake-receiver:8081/hook
SECRET="${SECRET:-demo_secret}"                             # must match ENDPOINT_SECRET in fake-receiver
CT="application/json"

# --- Helpers ---
need() { command -v "$1" >/dev/null || { echo "Missing dependency: $1"; exit 1; }; }
need curl
need openssl

hmac_hex() {
  # usage: hmac_hex "<body>" "<ts>" "<secret>"
  local body="$1" ts="$2" secret="$3"
  # Try binary+xxd, fallback to parsing OpenSSL's hex output
  if command -v xxd >/dev/null; then
    printf "%s%s" "$body" "$ts" | openssl dgst -sha256 -hmac "$secret" -binary | xxd -p -c 256
  else
    printf "%s%s" "$body" "$ts" | openssl dgst -sha256 -hmac "$secret" | awk '{print $2}'
  fi
}

do_req() {
  # usage: do_req "<name>" <expected_status> "<body>" "<ts>" "<sig_header_value or empty>" "<extra_headers>"
  local name="$1" expect="$2" body="$3" ts="$4" sig="$5" extra="${6:-}"
  local code
  if [[ -n "$sig" && -n "$ts" ]]; then
    code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$RECEIVER_URL" \
      -H "Content-Type: $CT" \
      -H "X-HarborHook-Timestamp: $ts" \
      -H "X-HarborHook-Signature: $sig" \
      $extra \
      --data "$body")
  else
    code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$RECEIVER_URL" \
      -H "Content-Type: $CT" \
      $extra \
      --data "$body")
  fi

  if [[ "$code" == "$expect" ]]; then
    echo "✅  $name — $code"
  else
    echo "❌  $name — got $code, expected $expect"
    exit 1
  fi
}

now_ts() { date +%s; }

# --- Tests ---

BODY='{"hello":"world"}'
TS=$(now_ts)
SIG_HEX=$(hmac_hex "$BODY" "$TS" "$SECRET")
SIG_HDR="sha256=$SIG_HEX"

echo "Testing against $RECEIVER_URL (secret: $SECRET)"

# 1) Happy path (valid)
do_req "valid signature" 200 "$BODY" "$TS" "$SIG_HDR"

# 2) Tampered body (uses signature from original body) -> 401
TAMPERED='{"hello":"wurld"}'
do_req "tampered body" 401 "$TAMPERED" "$TS" "$SIG_HDR"

# 3) Wrong secret -> 401
BAD_SIG="sha256=$(hmac_hex "$BODY" "$TS" "wrong_secret")"
do_req "wrong secret" 401 "$BODY" "$TS" "$BAD_SIG"

# 4) Stale timestamp (older than leeway) -> 401
OLD_TS=$(( $(now_ts) - 10000 ))
OLD_SIG="sha256=$(hmac_hex "$BODY" "$OLD_TS" "$SECRET")"
do_req "stale timestamp" 401 "$BODY" "$OLD_TS" "$OLD_SIG"

# 5) Missing headers -> 401
do_req "missing headers" 401 "$BODY" "" ""

# 6) Bad scheme in signature header -> 401
BAD_SCHEME="sha1=$(hmac_hex "$BODY" "$TS" "$SECRET")"
do_req "bad signature scheme" 401 "$BODY" "$TS" "$BAD_SCHEME"

echo "🎉 All signature tests passed."