# Webhook Signature Verification Guide

## Overview

Harborhook signs every webhook with an HMAC-SHA256 signature to ensure authenticity and integrity. This guide explains how to verify signatures in your webhook receiver application.

## Why Verify Signatures?

Without signature verification, your webhook endpoint is vulnerable to:

- **Spoofing**: Attackers could send fake webhooks pretending to be from Harborhook
- **Replay attacks**: Old webhooks could be resent maliciously
- **Data tampering**: Payload could be modified in transit

**Best Practice**: Always verify signatures in production webhook receivers.

## How Harborhook Signs Webhooks

When delivering a webhook, Harborhook includes two HTTP headers:

```http
X-HarborHook-Signature: sha256=a1b2c3d4e5f6...
X-HarborHook-Timestamp: 1699999999
```

### Signature Components

1. **Endpoint Secret**: Shared secret configured when you create the endpoint
2. **Payload Body**: The raw JSON body of the webhook (as bytes, no parsing)
3. **Timestamp**: Unix timestamp when the webhook was sent
4. **HMAC Algorithm**: SHA-256

### Signature Generation (How Harborhook Does It)

```
message = payload_body + timestamp_string
signature = HMAC-SHA256(message, endpoint_secret)
header_value = "sha256=" + hex(signature)
```

**Example**:
```
Payload: {"user_id":"123","event":"signup"}
Timestamp: 1699999999
Secret: my_webhook_secret

Message to sign: {"user_id":"123","event":"signup"}1699999999
Signature: HMAC-SHA256(message, "my_webhook_secret")
Header: sha256=a1b2c3d4e5f6789...
```

## Verification Steps

Your webhook receiver should:

1. **Extract headers**: Get `X-HarborHook-Signature` and `X-HarborHook-Timestamp`
2. **Check timestamp**: Verify webhook isn't too old (prevent replay attacks)
3. **Reconstruct message**: Concatenate raw body + timestamp
4. **Compute expected signature**: HMAC-SHA256(message, your_secret)
5. **Compare signatures**: Use constant-time comparison to prevent timing attacks
6. **Process webhook**: Only if signature matches

## Code Examples

### Python (Flask)

```python
import hmac
import hashlib
import time
from flask import Flask, request, jsonify

app = Flask(__name__)

# Your endpoint secret (from Harborhook endpoint configuration)
ENDPOINT_SECRET = "demo_secret"

# Maximum age of webhook in seconds (5 minutes)
TIMESTAMP_TOLERANCE = 300

def verify_signature(payload_body, signature_header, timestamp_header):
    """
    Verify the HMAC signature of a webhook.

    Args:
        payload_body: Raw request body as bytes
        signature_header: Value of X-HarborHook-Signature header
        timestamp_header: Value of X-HarborHook-Timestamp header

    Returns:
        True if signature is valid, False otherwise
    """
    # Check if timestamp is provided
    if not timestamp_header:
        return False

    # Check timestamp freshness (prevent replay attacks)
    try:
        webhook_timestamp = int(timestamp_header)
        current_timestamp = int(time.time())

        if abs(current_timestamp - webhook_timestamp) > TIMESTAMP_TOLERANCE:
            print(f"Webhook too old or timestamp invalid")
            return False
    except ValueError:
        print("Invalid timestamp format")
        return False

    # Extract signature from header (format: "sha256=hexdigest")
    if not signature_header or not signature_header.startswith("sha256="):
        print("Invalid signature header format")
        return False

    received_signature = signature_header[7:]  # Remove "sha256=" prefix

    # Construct the message that was signed
    # IMPORTANT: Use raw bytes, not parsed JSON
    message = payload_body + timestamp_header.encode('utf-8')

    # Compute expected signature
    expected_signature = hmac.new(
        ENDPOINT_SECRET.encode('utf-8'),
        message,
        hashlib.sha256
    ).hexdigest()

    # Compare signatures using constant-time comparison
    # This prevents timing attacks
    return hmac.compare_digest(expected_signature, received_signature)

@app.route('/webhook', methods=['POST'])
def handle_webhook():
    # Get headers
    signature = request.headers.get('X-HarborHook-Signature')
    timestamp = request.headers.get('X-HarborHook-Timestamp')

    # Get raw body (important: don't parse JSON yet)
    payload_body = request.get_data()

    # Verify signature
    if not verify_signature(payload_body, signature, timestamp):
        print("Signature verification failed")
        return jsonify({"error": "Invalid signature"}), 401

    print("Signature verified successfully")

    # Now it's safe to process the webhook
    payload = request.get_json()
    print(f"Processing webhook: {payload}")

    # Your business logic here
    # ...

    return jsonify({"status": "ok"}), 200

if __name__ == '__main__':
    app.run(port=8081)
```

### Go (net/http)

```go
package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "strconv"
    "strings"
    "time"
)

const (
    endpointSecret     = "demo_secret"
    timestampTolerance = 300 // 5 minutes in seconds
)

type WebhookPayload struct {
    UserID string `json:"user_id"`
    Event  string `json:"event"`
    // Add your expected fields
}

func verifySignature(body []byte, signatureHeader, timestampHeader string) bool {
    // Check timestamp freshness
    webhookTimestamp, err := strconv.ParseInt(timestampHeader, 10, 64)
    if err != nil {
        log.Printf("Invalid timestamp format: %v", err)
        return false
    }

    currentTimestamp := time.Now().Unix()
    if abs(currentTimestamp-webhookTimestamp) > timestampTolerance {
        log.Printf("Webhook too old or timestamp invalid")
        return false
    }

    // Extract signature (format: "sha256=hexdigest")
    if !strings.HasPrefix(signatureHeader, "sha256=") {
        log.Printf("Invalid signature header format")
        return false
    }
    receivedSig := signatureHeader[7:]

    // Construct message
    message := append(body, []byte(timestampHeader)...)

    // Compute expected signature
    mac := hmac.New(sha256.New, []byte(endpointSecret))
    mac.Write(message)
    expectedSig := hex.EncodeToString(mac.Sum(nil))

    // Constant-time comparison
    return subtle.ConstantTimeCompare(
        []byte(expectedSig),
        []byte(receivedSig),
    ) == 1
}

func abs(n int64) int64 {
    if n < 0 {
        return -n
    }
    return n
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
    // Read body
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "Failed to read body", http.StatusBadRequest)
        return
    }
    defer r.Body.Close()

    // Get headers
    signature := r.Header.Get("X-HarborHook-Signature")
    timestamp := r.Header.Get("X-HarborHook-Timestamp")

    // Verify signature
    if !verifySignature(body, signature, timestamp) {
        log.Printf("Signature verification failed")
        http.Error(w, "Invalid signature", http.StatusUnauthorized)
        return
    }

    log.Printf("Signature verified successfully")

    // Parse payload
    var payload WebhookPayload
    if err := json.Unmarshal(body, &payload); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }

    // Process webhook
    log.Printf("Processing webhook: %+v", payload)

    // Your business logic here

    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, `{"status":"ok"}`)
}

func main() {
    http.HandleFunc("/webhook", webhookHandler)
    log.Println("Webhook server listening on :8081")
    log.Fatal(http.ListenAndServe(":8081", nil))
}
```

### Node.js (Express)

```javascript
const express = require('express');
const crypto = require('crypto');

const app = express();

// Your endpoint secret
const ENDPOINT_SECRET = 'demo_secret';
const TIMESTAMP_TOLERANCE = 300; // 5 minutes in seconds

// Middleware to get raw body (needed for signature verification)
app.use(express.json({
    verify: (req, res, buf) => {
        req.rawBody = buf.toString('utf8');
    }
}));

function verifySignature(rawBody, signatureHeader, timestampHeader) {
    // Check timestamp
    const webhookTimestamp = parseInt(timestampHeader, 10);
    const currentTimestamp = Math.floor(Date.now() / 1000);

    if (Math.abs(currentTimestamp - webhookTimestamp) > TIMESTAMP_TOLERANCE) {
        console.log('Webhook too old or timestamp invalid');
        return false;
    }

    // Extract signature (format: "sha256=hexdigest")
    if (!signatureHeader || !signatureHeader.startsWith('sha256=')) {
        console.log('Invalid signature header format');
        return false;
    }
    const receivedSignature = signatureHeader.substring(7);

    // Construct message
    const message = rawBody + timestampHeader;

    // Compute expected signature
    const hmac = crypto.createHmac('sha256', ENDPOINT_SECRET);
    hmac.update(message);
    const expectedSignature = hmac.digest('hex');

    // Constant-time comparison
    return crypto.timingSafeEqual(
        Buffer.from(expectedSignature, 'hex'),
        Buffer.from(receivedSignature, 'hex')
    );
}

app.post('/webhook', (req, res) => {
    const signature = req.headers['x-harborhook-signature'];
    const timestamp = req.headers['x-harborhook-timestamp'];
    const rawBody = req.rawBody;

    // Verify signature
    if (!verifySignature(rawBody, signature, timestamp)) {
        console.log('Signature verification failed');
        return res.status(401).json({ error: 'Invalid signature' });
    }

    console.log('Signature verified successfully');

    // Process webhook
    const payload = req.body;
    console.log('Processing webhook:', payload);

    // Your business logic here

    res.json({ status: 'ok' });
});

app.listen(8081, () => {
    console.log('Webhook server listening on port 8081');
});
```

## Testing Your Implementation

### Using the Test Script

Harborhook includes a test script to verify your signature implementation:

```bash
# Test against your local webhook receiver
cd harbor_hook
export RECEIVER_URL="http://localhost:8081/webhook"
export SECRET="demo_secret"

./scripts/test_signature.sh
```

**Expected output:**
```
Testing against http://localhost:8081/webhook (secret: demo_secret)
valid signature — 200
tampered body — 401
wrong secret — 401
stale timestamp — 401
missing headers — 401
bad signature scheme — 401
All signature tests passed.
```

### Manual Testing

```bash
# Generate a valid signature manually
BODY='{"test":"data"}'
TIMESTAMP=$(date +%s)
SECRET="demo_secret"

# Compute signature
SIGNATURE=$(printf "%s%s" "$BODY" "$TIMESTAMP" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')

# Send webhook with signature
curl -X POST http://localhost:8081/webhook \
  -H "Content-Type: application/json" \
  -H "X-HarborHook-Signature: sha256=$SIGNATURE" \
  -H "X-HarborHook-Timestamp: $TIMESTAMP" \
  -d "$BODY"
```

## Common Pitfalls

### Don't: Parse JSON Before Verification

```python
# WRONG - Don't do this!
payload = request.get_json()
body_string = json.dumps(payload)
message = body_string + timestamp
```

**Why**: JSON parsing can reorder keys or change formatting. Use the raw body bytes.

### Do: Use Raw Request Body

```python
# CORRECT
payload_body = request.get_data()  # Raw bytes
message = payload_body + timestamp.encode('utf-8')
```

### Don't: Use String Comparison

```python
# WRONG - Vulnerable to timing attacks
if expected_signature == received_signature:
    return True
```

**Why**: Standard string comparison reveals information about how many characters match, enabling timing attacks.

### Do: Use Constant-Time Comparison

```python
# CORRECT
return hmac.compare_digest(expected_signature, received_signature)
```

### Don't: Skip Timestamp Validation

```python
# WRONG - Vulnerable to replay attacks
# Just verifying signature without checking timestamp
```

**Why**: Attackers could resend old valid webhooks indefinitely.

### Do: Validate Timestamp

```python
# CORRECT
current_time = time.time()
if abs(current_time - webhook_timestamp) > 300:  # 5 minutes
    return False
```

## Security Best Practices

### 1. Keep Secrets Secure

- Store secrets in environment variables or secret management systems
- Use different secrets for different environments (dev, staging, prod)
- Never commit secrets to version control
- Never log secrets

### 2. Use HTTPS

- Always configure webhook endpoints with HTTPS URLs
- Use valid TLS certificates
- Don't use HTTP for webhook receivers in production

### 3. Validate Timestamps

- Reject webhooks older than 5 minutes (configurable)
- Log suspicious timestamp patterns
- Monitor for replay attack attempts

### 4. Use Constant-Time Comparison

- Use language-provided secure comparison functions:
  - Python: `hmac.compare_digest()`
  - Go: `subtle.ConstantTimeCompare()`
  - Node.js: `crypto.timingSafeEqual()`
  - PHP: `hash_equals()`
- Never use `==` or `===` for signature comparison

### 5. Handle Errors Gracefully

```python
@app.route('/webhook', methods=['POST'])
def handle_webhook():
    try:
        # Verify signature
        if not verify_signature(...):
            return jsonify({"error": "Invalid signature"}), 401

        # Process webhook
        process_webhook(payload)

        return jsonify({"status": "ok"}), 200

    except Exception as e:
        # Log error but don't expose internals
        log.error(f"Webhook processing failed: {e}")
        return jsonify({"error": "Internal error"}), 500
```

### 6. Rate Limiting

Implement rate limiting on your webhook endpoint to prevent abuse:

```python
from flask_limiter import Limiter

limiter = Limiter(app, key_func=lambda: request.remote_addr)

@app.route('/webhook', methods=['POST'])
@limiter.limit("100/minute")
def handle_webhook():
    # ...
```

## Debugging Signature Issues

### Enable Verbose Logging

```python
def verify_signature(payload_body, signature_header, timestamp_header):
    print(f"DEBUG: Received signature: {signature_header}")
    print(f"DEBUG: Received timestamp: {timestamp_header}")
    print(f"DEBUG: Body length: {len(payload_body)}")
    print(f"DEBUG: Body (first 100 chars): {payload_body[:100]}")

    # Compute expected signature
    message = payload_body + timestamp_header.encode('utf-8')
    expected_signature = hmac.new(
        ENDPOINT_SECRET.encode('utf-8'),
        message,
        hashlib.sha256
    ).hexdigest()

    print(f"DEBUG: Expected signature: sha256={expected_signature}")
    print(f"DEBUG: Signatures match: {hmac.compare_digest(expected_signature, signature_header[7:])}")

    # ... rest of verification
```

### Common Issues and Solutions

| Issue | Cause | Solution |
|-------|-------|----------|
| Signature always fails | Wrong secret | Verify secret matches endpoint configuration |
| Timestamp validation fails | Clock skew | Sync server clocks or increase tolerance |
| Signature fails intermittently | Body parsing before verification | Use raw body bytes, don't parse JSON first |
| Works in curl but not in code | Character encoding | Ensure UTF-8 encoding throughout |

## Configuration in Harborhook

When creating an endpoint, specify your secret:

```bash
# Using harborctl
harborctl endpoint create tn_demo https://your-app.com/webhook \
  --secret "your_secure_secret_here"

# Using API
curl -X POST https://harborhook.example.com/v1/tenants/tn_demo/endpoints \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tn_demo",
    "url": "https://your-app.com/webhook",
    "secret": "your_secure_secret_here"
  }'
```

**Secret Requirements**:
- Minimum 16 characters recommended
- Use cryptographically random strings
- Different secret per endpoint (optional but recommended)

## Reference: Signature Algorithm

For implementers, here's the precise algorithm:

```
INPUTS:
  payload_body: Raw HTTP request body as bytes
  timestamp: Unix timestamp as string (e.g., "1699999999")
  secret: Endpoint secret as string

ALGORITHM:
  1. message = payload_body || timestamp
     (concatenate body bytes and timestamp string bytes)
  2. signature_bytes = HMAC-SHA256(message, secret)
  3. signature_hex = hex_encode(signature_bytes)
  4. header_value = "sha256=" || signature_hex

VERIFICATION:
  1. Extract received_signature from "X-HarborHook-Signature" header
     (remove "sha256=" prefix)
  2. Extract timestamp from "X-HarborHook-Timestamp" header
  3. Verify timestamp freshness (current_time - timestamp < 300 seconds)
  4. Recompute signature using same algorithm
  5. Compare using constant-time comparison:
     constant_time_compare(recomputed_signature, received_signature)
  6. Accept webhook ONLY if comparison returns true
```

## Related Documentation

- [Architecture Overview](./architecture.md) - How Harborhook works
- [Harborctl CLI Guide](./harborctl.md) - Managing endpoints and secrets
- [Quickstart Guide](./QUICKSTART.md) - Getting started with Harborhook
- [Worker Source Code](../cmd/worker/main.go) - See how Harborhook signs webhooks

## Need Help?

- **Issues**: [GitHub Issues](https://github.com/austindbirch/harbor_hook/issues)
- **Security Issues**: Please report via private security advisory
- **Documentation**: [docs/](./README.md)

---

**Remember**: Signature verification is your first line of defense. Always verify signatures in production!
