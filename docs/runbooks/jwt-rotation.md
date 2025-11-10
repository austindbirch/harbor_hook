# Runbook: JWT Key Rotation

## Overview
This runbook covers the procedure for rotating JWT signing keys used for API authentication in Harborhook. Regular key rotation is a security best practice that limits the impact of potential key compromise.

## Severity
**Medium** (Scheduled) / **High** (Emergency rotation after compromise)

## When to Rotate
- **Scheduled**: Every 90 days as part of security hygiene
- **Emergency**: Immediately if key compromise suspected
- **Operational**: Before key expiry date
- **Compliance**: As required by security policies or audits

## Prerequisites
- Access to jwks-server configuration and secrets
- Ability to generate RSA keypairs
- Access to Kubernetes cluster or Docker environment
- Backup of current keys

## JWT Architecture Overview
Harborhook uses RS256 (RSA with SHA-256) for JWT signing:
- **Private Key**: Held by jwks-server, used to sign tokens
- **Public Key**: Exposed via JWKS endpoint, used by Envoy to verify tokens
- **Key ID (kid)**: Identifies which key was used to sign a token
- **Rotation Window**: Period where both old and new keys are valid

## Rotation Strategy
We use **key overlap** to ensure zero-downtime rotation:
1. Generate new keypair with new `kid`
2. Add new public key to JWKS endpoint (now serves both keys)
3. Update jwks-server to sign new tokens with new key
4. Wait for all old tokens to expire (default TTL: 1 hour)
5. Remove old public key from JWKS endpoint

## Scheduled Rotation Procedure

### Step 1: Generate New Keypair
```bash
# Set key identifier (use date-based naming)
NEW_KID="harborhook-$(date +%Y%m%d)"

# Generate new RSA keypair (4096-bit)
openssl genrsa -out jwt-private-${NEW_KID}.pem 4096

# Extract public key
openssl rsa -in jwt-private-${NEW_KID}.pem -pubout -out jwt-public-${NEW_KID}.pem

# Verify keypair
openssl rsa -in jwt-private-${NEW_KID}.pem -check -noout
# Should output: RSA key ok

# Set restrictive permissions
chmod 600 jwt-private-${NEW_KID}.pem
chmod 644 jwt-public-${NEW_KID}.pem
```

### Step 2: Backup Current Keys
```bash
# In Kubernetes
kubectl get secret test-harborhook-jwks-keys -o yaml > jwks-keys-backup-$(date +%Y%m%d).yaml

# Verify backup
cat jwks-keys-backup-*.yaml

# Store backup securely (e.g., encrypted vault, KMS)
```

### Step 3: Update jwks-server Configuration
```bash
# Add new key to jwks-server (keeping old key)
# The server should now serve BOTH public keys in JWKS endpoint

# In Kubernetes - update secret with new keys
kubectl create secret generic test-harborhook-jwks-keys-new \
  --from-file=current-private=jwt-private-${NEW_KID}.pem \
  --from-file=current-public=jwt-public-${NEW_KID}.pem \
  --from-file=previous-private=jwt-private-OLD_KID.pem \
  --from-file=previous-public=jwt-public-OLD_KID.pem \
  --dry-run=client -o yaml | kubectl apply -f -

# Or for Docker Compose, update volume mounts in docker-compose.yaml
```

### Step 4: Update jwks-server to Use New Key for Signing
```bash
# Update jwks-server configuration to sign with new key
kubectl set env deployment/test-harborhook-jwks-server \
  CURRENT_KEY_ID=${NEW_KID}

# Restart jwks-server to pick up new configuration
kubectl rollout restart deployment/test-harborhook-jwks-server

# Wait for rollout
kubectl rollout status deployment/test-harborhook-jwks-server
```

### Step 5: Verify JWKS Endpoint Serves Both Keys
```bash
# Query JWKS endpoint
curl -s http://localhost:8082/.well-known/jwks.json | jq

# Should see both keys listed with different 'kid' values
# Example output:
# {
#   "keys": [
#     {
#       "kty": "RSA",
#       "kid": "harborhook-20250109",  # NEW KEY
#       "use": "sig",
#       "n": "...",
#       "e": "AQAB"
#     },
#     {
#       "kty": "RSA",
#       "kid": "harborhook-20241109",  # OLD KEY (still valid)
#       "use": "sig",
#       "n": "...",
#       "e": "AQAB"
#     }
#   ]
# }
```

### Step 6: Verify New Tokens Use New Key
```bash
# Request a new token
TOKEN=$(curl -s -X POST "http://localhost:8082/token" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tn_demo"}' | jq -r '.token')

# Decode token header (without verification)
echo $TOKEN | cut -d. -f1 | base64 -d 2>/dev/null | jq

# Verify 'kid' in header matches NEW_KID
# Example output:
# {
#   "alg": "RS256",
#   "kid": "harborhook-20250109",  # Should be NEW_KID
#   "typ": "JWT"
# }
```

### Step 7: Verify Old Tokens Still Work
```bash
# If you have an old token saved, test it
OLD_TOKEN="eyJhbGciOi..."

# Use old token to make API request
curl -sk -X GET "https://localhost:8443/v1/ping" \
  -H "Authorization: Bearer $OLD_TOKEN"

# Should succeed (200 OK) because old public key still in JWKS
```

### Step 8: Wait for Old Tokens to Expire
```bash
# Check token TTL (default: 1 hour)
echo $OLD_TOKEN | cut -d. -f2 | base64 -d 2>/dev/null | jq '.exp'

# Calculate time remaining
EXP=$(echo $OLD_TOKEN | cut -d. -f2 | base64 -d 2>/dev/null | jq -r '.exp')
NOW=$(date +%s)
REMAINING=$((EXP - NOW))
echo "Old token expires in $((REMAINING / 60)) minutes"

# Wait for all old tokens to expire (TTL + buffer)
# Recommended: Wait TTL + 15 minutes
# For 1-hour TTL: wait 75 minutes total
sleep 4500  # 75 minutes
```

### Step 9: Remove Old Public Key from JWKS
```bash
# Update secret to remove old key
kubectl create secret generic test-harborhook-jwks-keys \
  --from-file=current-private=jwt-private-${NEW_KID}.pem \
  --from-file=current-public=jwt-public-${NEW_KID}.pem \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart jwks-server
kubectl rollout restart deployment/test-harborhook-jwks-server
kubectl rollout status deployment/test-harborhook-jwks-server
```

### Step 10: Verify JWKS Endpoint Only Serves New Key
```bash
# Query JWKS endpoint
curl -s http://localhost:8082/.well-known/jwks.json | jq

# Should only see new key
# {
#   "keys": [
#     {
#       "kty": "RSA",
#       "kid": "harborhook-20250109",
#       "use": "sig",
#       "n": "...",
#       "e": "AQAB"
#     }
#   ]
# }

# Verify old token no longer works
curl -sk -X GET "https://localhost:8443/v1/ping" \
  -H "Authorization: Bearer $OLD_TOKEN"
# Should fail with 401 Unauthorized
```

### Step 11: Document and Archive
```bash
# Document rotation in audit log
cat >> /var/log/harborhook/key-rotation.log <<EOF
Date: $(date -Iseconds)
Action: JWT Key Rotation
Old Key ID: harborhook-20241109
New Key ID: ${NEW_KID}
Performed By: $(whoami)
Reason: Scheduled 90-day rotation
Status: Completed
EOF

# Securely archive old keys (for audit/forensics, not for use)
# Encrypt and move to secure storage
tar czf jwt-keys-archive-$(date +%Y%m%d).tar.gz jwt-private-OLD_KID.pem jwt-public-OLD_KID.pem
openssl enc -aes-256-cbc -salt -in jwt-keys-archive-*.tar.gz -out jwt-keys-archive-*.tar.gz.enc
rm jwt-keys-archive-*.tar.gz

# Delete unencrypted old keys from local system
shred -u jwt-private-OLD_KID.pem jwt-public-OLD_KID.pem
```

## Emergency Rotation (Key Compromise)

If you suspect key compromise, follow expedited procedure:

### 1. Immediate Response
```bash
# Generate new key immediately (follow Step 1 above)
# Mark incident with high priority
INCIDENT_ID="SEC-$(date +%Y%m%d-%H%M%S)"

# Document compromise
cat >> /var/log/harborhook/security-incidents.log <<EOF
Incident: ${INCIDENT_ID}
Date: $(date -Iseconds)
Type: JWT Key Compromise (suspected)
Action: Emergency key rotation initiated
EOF
```

### 2. Accelerated Rotation
```bash
# Follow Steps 2-7 above, but:

# DO NOT wait for old tokens to expire
# Immediately remove old key from JWKS (Step 9)
# This will invalidate ALL existing tokens

# Force clients to re-authenticate
kubectl rollout restart deployment/test-harborhook-jwks-server
kubectl rollout restart deployment/test-harborhook-envoy
```

### 3. Notify Stakeholders
```
Subject: [URGENT] JWT Key Rotation - Re-authentication Required

Due to a security event, we have performed an emergency JWT key rotation.

Impact:
- All existing JWT tokens are now invalid
- API clients must obtain new tokens immediately
- Webhook deliveries may be delayed during re-authentication

Action Required:
- Obtain new JWT token from: POST /token
- Update your API clients with new token
- Monitor for authentication errors

Timeline:
- Rotation completed: [TIMESTAMP]
- Service restoration: Immediate

We apologize for the disruption and appreciate your understanding.
```

## Rollback Procedure

If rotation causes unexpected issues:

```bash
# Restore old keys from backup
kubectl apply -f jwks-keys-backup-YYYYMMDD.yaml

# Restart services
kubectl rollout restart deployment/test-harborhook-jwks-server
kubectl rollout restart deployment/test-harborhook-envoy

# Verify JWKS endpoint
curl -s http://localhost:8082/.well-known/jwks.json | jq

# Test old token
curl -sk -X GET "https://localhost:8443/v1/ping" \
  -H "Authorization: Bearer $OLD_TOKEN"

# Document rollback
cat >> /var/log/harborhook/key-rotation.log <<EOF
Date: $(date -Iseconds)
Action: JWT Key Rotation Rollback
Reason: [REASON]
Status: Rolled back to previous keys
EOF
```

## Validation Checklist

After rotation:
- [ ] JWKS endpoint accessible and returns expected keys
- [ ] New tokens can be obtained successfully
- [ ] New tokens authenticate successfully against APIs
- [ ] Envoy accepts tokens signed with new key
- [ ] Old tokens rejected after expiry (scheduled rotation)
- [ ] No authentication errors in logs
- [ ] Metrics show normal token validation rate
- [ ] Customer-facing services functioning normally

## Monitoring

```bash
# Monitor token validation errors
curl -s 'http://localhost:9090/api/v1/query?query=rate(envoy_jwt_authn_denied_total[5m])' | jq

# Check jwks-server health
curl -s http://localhost:8082/healthz | jq

# Monitor API gateway errors
kubectl logs -l app.kubernetes.io/component=envoy --tail=100 | grep -i jwt
```

## Automation

For scheduled rotations, consider automating:

```bash
#!/bin/bash
# jwt-auto-rotate.sh - Automated JWT key rotation

# This script should be:
# - Scheduled via cron (every 90 days)
# - Run with proper security controls
# - Monitored for success/failure
# - Include notifications

# See full implementation in /scripts/security/jwt-rotate.sh
```

## Security Best Practices

1. **Key Storage**: Store private keys in Kubernetes Secrets or KMS, never in version control
2. **Key Strength**: Use 4096-bit RSA keys minimum
3. **Key ID Format**: Use timestamp-based naming for auditability
4. **Rotation Frequency**: Every 90 days for scheduled, immediately for compromise
5. **Access Control**: Limit who can perform key rotation (RBAC)
6. **Audit Trail**: Log all rotation events with timestamp and operator
7. **Key Archival**: Encrypt and securely store old keys for forensics
8. **Testing**: Test rotation procedure in staging before production

## Troubleshooting

### Issue: New Tokens Fail Validation
```bash
# Check Envoy JWKS cache
# Envoy caches JWKS responses, may need time to refresh
# Default cache duration: 5 minutes

# Force Envoy pod restart to clear cache
kubectl rollout restart deployment/test-harborhook-envoy

# Or wait for cache TTL to expire
sleep 300
```

### Issue: JWKS Endpoint Not Responding
```bash
# Check jwks-server logs
kubectl logs -l app.kubernetes.io/component=jwks-server --tail=50

# Check jwks-server pod status
kubectl get pods -l app.kubernetes.io/component=jwks-server

# Verify secret is mounted correctly
kubectl exec test-harborhook-jwks-server-xxx -- ls -la /etc/jwks/
```

### Issue: Clients Cannot Get Tokens
```bash
# Check jwks-server health endpoint
curl -s http://localhost:8082/healthz

# Test token endpoint
curl -v -X POST "http://localhost:8082/token" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tn_demo"}'

# Check for errors in response
```

## References
- [JWT RFC 7519](https://tools.ietf.org/html/rfc7519)
- [JWKS RFC 7517](https://tools.ietf.org/html/rfc7517)
- [Envoy JWT Authentication](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/jwt_authn_filter)
- [Harborhook Auth Architecture](../architecture.md#authentication)

## Rotation Schedule

| Rotation Date | Key ID | Performed By | Notes |
|---------------|--------|--------------|-------|
| 2024-11-09    | harborhook-20241109 | Initial | First production key |
| 2025-02-07    | TBD | TBD | 90-day scheduled rotation |

## Post-Rotation Checklist
- [ ] Rotation completed successfully
- [ ] Validation tests passed
- [ ] Old keys securely archived
- [ ] Audit log updated
- [ ] Rotation schedule updated
- [ ] Next rotation scheduled (90 days)
- [ ] Stakeholders notified (if applicable)
- [ ] Runbook updated with lessons learned
