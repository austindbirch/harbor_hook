# Runbook: TLS Certificate Rotation

## Overview
This runbook covers the procedure for rotating TLS/mTLS certificates used in Harborhook for secure communication between components and with external clients.

## Severity
**High** - Expired certificates will cause service outages. Rotation must be completed before expiry.

## Certificate Architecture

Harborhook uses certificates for:
1. **Envoy HTTPS Termination**: External clients → Envoy (port 8443)
2. **mTLS between services**: Envoy → Ingest/Worker (internal)

Certificate structure:
```
Root CA (ca.crt, ca.key)
├── Server Certificate (server.crt, server.key)
│   └── Used by: Envoy for HTTPS termination
└── Client Certificate (client.crt, client.key)
    └── Used by: Envoy for mTLS to internal services
```

## When to Rotate
- **Scheduled**: 30 days before certificate expiry
- **Emergency**: Immediately if certificates compromised
- **Operational**: After security incidents or policy changes
- **Compliance**: As required by security audits

## Prerequisites
- Access to certificate generation scripts
- Kubernetes cluster access (or Docker Compose environment)
- Backup of current certificates
- Understanding of certificate chain validation

## Pre-Rotation Checks

### Check Certificate Expiry
```bash
# For Docker Compose environment
cd deploy/docker/envoy/certs

# Check server certificate expiry
openssl x509 -in server.crt -noout -enddate
# Output: notAfter=Feb  7 12:00:00 2025 GMT

# Check client certificate expiry
openssl x509 -in client.crt -noout -enddate

# Check CA certificate expiry
openssl x509 -in ca.crt -noout -enddate

# Calculate days until expiry
openssl x509 -in server.crt -noout -checkend $((30*86400))
# Exit code 0 = valid for 30+ days
# Exit code 1 = expires within 30 days

# For Kubernetes
kubectl get secret harborhook-certs -o json | \
  jq -r '.data["server.crt"]' | base64 -d | \
  openssl x509 -noout -enddate
```

### Verify Current Certificate Health
```bash
# Test HTTPS endpoint with current cert
curl -vk https://localhost:8443/v1/ping 2>&1 | grep -E "subject|issuer|expire"

# Check certificate chain
openssl s_client -connect localhost:8443 -showcerts </dev/null 2>/dev/null | \
  openssl x509 -noout -text | grep -E "Issuer|Subject|Not After"

# Verify mTLS is working
kubectl logs -l app.kubernetes.io/component=envoy --tail=100 | grep -i tls
```

## Rotation Procedure (Docker Compose)

### Step 1: Backup Current Certificates
```bash
cd deploy/docker/envoy/certs

# Backup existing certificates
BACKUP_DIR="certs-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"
cp ca.crt ca.key server.crt server.key client.crt client.key "$BACKUP_DIR/"

# Verify backup
ls -la "$BACKUP_DIR/"

# Archive and secure backup
tar czf "${BACKUP_DIR}.tar.gz" "$BACKUP_DIR"
chmod 600 "${BACKUP_DIR}.tar.gz"
```

### Step 2: Generate New Certificates
```bash
# Use existing certificate generation script
./generate_certs.sh

# The script will:
# 1. Generate new CA (or reuse existing if still valid)
# 2. Generate new server certificate
# 3. Generate new client certificate
# 4. Set proper permissions

# Verify new certificates
openssl x509 -in server.crt -noout -text | grep -E "Subject:|Issuer:|Not After"
openssl x509 -in client.crt -noout -text | grep -E "Subject:|Issuer:|Not After"

# Verify certificate chain
openssl verify -CAfile ca.crt server.crt
# Should output: server.crt: OK

openssl verify -CAfile ca.crt client.crt
# Should output: client.crt: OK
```

### Step 3: Restart Services
```bash
# Stop services
cd ../..  # Back to deploy/docker
docker-compose down

# Start services with new certificates
docker-compose up -d

# Wait for services to stabilize
sleep 10

# Check service health
docker-compose ps
docker-compose logs envoy | tail -20
```

### Step 4: Verify New Certificates
```bash
# Test HTTPS endpoint
curl -vk https://localhost:8443/v1/ping 2>&1 | grep -E "subject|issuer|expire"

# Get JWT token and test authenticated endpoint
TOKEN=$(curl -s -X POST "http://localhost:8082/token" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tn_demo"}' | jq -r '.token')

curl -sk -X GET "https://localhost:8443/v1/ping" \
  -H "Authorization: Bearer $TOKEN"
# Should return: {"message":"pong"}

# Check mTLS between Envoy and internal services
docker-compose logs envoy | grep -i "tls\|ssl\|certificate" | tail -20
docker-compose logs ingest | grep -i "tls\|ssl\|certificate" | tail -20
```

## Rotation Procedure (Kubernetes)

### Step 1: Backup Current Secret
```bash
# Export current certificate secret
kubectl get secret harborhook-certs -o yaml > certs-backup-$(date +%Y%m%d-%H%M%S).yaml

# Verify backup
cat certs-backup-*.yaml

# Extract and view current certificate details
kubectl get secret harborhook-certs -o json | \
  jq -r '.data["server.crt"]' | base64 -d | \
  openssl x509 -noout -text | head -20
```

### Step 2: Generate New Certificates Locally
```bash
# Use the cert generation script
cd deploy/docker/envoy/certs
./generate_certs.sh

# Verify new certificates
openssl verify -CAfile ca.crt server.crt
openssl verify -CAfile ca.crt client.crt
```

### Step 3: Update Kubernetes Secret
```bash
# Delete old secret
kubectl delete secret harborhook-certs

# Create new secret with new certificates
kubectl create secret generic harborhook-certs \
  --from-file=ca.crt=./ca.crt \
  --from-file=server.crt=./server.crt \
  --from-file=server.key=./server.key \
  --from-file=client.crt=./client.crt \
  --from-file=client.key=./client.key

# Verify secret was created
kubectl get secret harborhook-certs
kubectl describe secret harborhook-certs
```

### Step 4: Rolling Restart of Services
```bash
# Restart Envoy (will pick up new certs)
kubectl rollout restart deployment test-harborhook-envoy
kubectl rollout status deployment test-harborhook-envoy

# Restart services that use mTLS
kubectl rollout restart deployment test-harborhook-ingest
kubectl rollout status deployment test-harborhook-ingest

kubectl rollout restart deployment test-harborhook-worker
kubectl rollout status deployment test-harborhook-worker

# Verify all pods are running
kubectl get pods
```

### Step 5: Verify New Certificates in Kubernetes
```bash
# Port-forward to Envoy
kubectl port-forward svc/test-harborhook-envoy 8443:8443 &
PF_PID=$!
sleep 3

# Test HTTPS endpoint
curl -vk https://localhost:8443/v1/ping 2>&1 | grep -E "subject|issuer|expire"

# Get JWT token
TOKEN=$(curl -s -X POST "http://localhost:8082/token" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tn_demo"}' | jq -r '.token')

# Test authenticated endpoint
curl -sk -X GET "https://localhost:8443/v1/ping" \
  -H "Authorization: Bearer $TOKEN"

# Cleanup port-forward
kill $PF_PID

# Check service logs for TLS errors
kubectl logs -l app.kubernetes.io/component=envoy --tail=50 | grep -i "tls\|certificate"
kubectl logs -l app.kubernetes.io/component=ingest --tail=50 | grep -i "tls\|certificate"
```

## Emergency Rotation (Certificate Compromise)

If certificates are compromised:

### 1. Immediate Actions
```bash
# Document incident
INCIDENT_ID="SEC-CERT-$(date +%Y%m%d-%H%M%S)"
cat >> /var/log/harborhook/security-incidents.log <<EOF
Incident: ${INCIDENT_ID}
Date: $(date -Iseconds)
Type: TLS Certificate Compromise (suspected)
Action: Emergency certificate rotation initiated
EOF

# Immediately revoke compromised certificates (if using CA infrastructure)
# For self-signed certs, rotation is the mitigation
```

### 2. Expedited Rotation
```bash
# Follow standard rotation procedure but:
# - Generate completely new CA (don't reuse)
# - Use new certificate serial numbers
# - Minimize service downtime window

# For Docker Compose:
cd deploy/docker/envoy/certs
rm ca.* server.* client.*  # Remove ALL certs
./generate_certs.sh  # Generate fresh CA and certs
cd ../..
docker-compose restart

# For Kubernetes:
# Follow Steps 2-4 above but with full CA regeneration
```

### 3. Verify No Old Certificate Usage
```bash
# Check all services are using new certificates
# Monitor logs for any TLS handshake failures with old certs

# In Kubernetes
kubectl logs -l app.kubernetes.io/component=envoy --tail=100 | \
  grep -i "certificate\|handshake" | grep -i "error\|fail"

# Should see no errors related to old certificates
```

## Rollback Procedure

If new certificates cause issues:

```bash
# For Docker Compose
cd deploy/docker/envoy/certs

# Restore certificates from backup
tar xzf certs-backup-TIMESTAMP.tar.gz
cp certs-backup-TIMESTAMP/* .

# Restart services
cd ../..
docker-compose restart

# Verify services
curl -vk https://localhost:8443/v1/ping

# For Kubernetes
# Restore secret from backup
kubectl apply -f certs-backup-TIMESTAMP.yaml

# Restart services
kubectl rollout restart deployment test-harborhook-envoy
kubectl rollout restart deployment test-harborhook-ingest
kubectl rollout restart deployment test-harborhook-worker

# Verify
kubectl get pods
```

## Certificate Monitoring and Alerts

### Prometheus Alert Rule
```yaml
# Add to prometheus alert rules
- alert: CertificateExpiringSoon
  expr: |
    (ssl_certificate_expiry_seconds - time()) / 86400 < 30
  labels:
    severity: warning
  annotations:
    summary: "TLS certificate expires in {{ $value }} days"
    description: "Certificate for {{ $labels.instance }} expires soon. Plan rotation."

- alert: CertificateExpiredOrExpiringSoon
  expr: |
    (ssl_certificate_expiry_seconds - time()) / 86400 < 7
  labels:
    severity: critical
  annotations:
    summary: "TLS certificate expires in {{ $value }} days - URGENT"
    description: "Certificate for {{ $labels.instance }} requires immediate rotation"
```

### Manual Certificate Monitoring
```bash
# Create monitoring script
cat > /usr/local/bin/check-cert-expiry.sh <<'EOF'
#!/bin/bash
CERT_PATH="/path/to/server.crt"
DAYS_WARNING=30
DAYS_CRITICAL=7

EXPIRY=$(openssl x509 -in $CERT_PATH -noout -enddate | cut -d= -f2)
EXPIRY_EPOCH=$(date -d "$EXPIRY" +%s)
NOW_EPOCH=$(date +%s)
DAYS_LEFT=$(( ($EXPIRY_EPOCH - $NOW_EPOCH) / 86400 ))

if [ $DAYS_LEFT -lt $DAYS_CRITICAL ]; then
  echo "CRITICAL: Certificate expires in $DAYS_LEFT days!"
  exit 2
elif [ $DAYS_LEFT -lt $DAYS_WARNING ]; then
  echo "WARNING: Certificate expires in $DAYS_LEFT days"
  exit 1
else
  echo "OK: Certificate expires in $DAYS_LEFT days"
  exit 0
fi
EOF

chmod +x /usr/local/bin/check-cert-expiry.sh

# Run via cron daily
# 0 9 * * * /usr/local/bin/check-cert-expiry.sh | mail -s "Cert Expiry Check" ops@example.com
```

## Validation Checklist

After rotation:
- [ ] New certificates generated successfully
- [ ] Certificate chain validates (ca → server/client)
- [ ] HTTPS endpoint accessible (port 8443)
- [ ] JWT authentication works through HTTPS
- [ ] mTLS communication working (Envoy ↔ services)
- [ ] No TLS handshake errors in logs
- [ ] Certificate expiry date is in the future (>90 days recommended)
- [ ] Old certificates backed up and archived
- [ ] Services restarted successfully
- [ ] Customer-facing APIs functioning normally

## Troubleshooting

### Issue: TLS Handshake Failures
```bash
# Check certificate chain
openssl s_client -connect localhost:8443 -CAfile ca.crt -showcerts </dev/null

# Common issues:
# - Certificate not trusted by CA
# - Hostname mismatch (check Subject Alternative Name)
# - Certificate expired

# Verify certificate details
openssl x509 -in server.crt -noout -text | grep -E "Subject|Issuer|DNS|Not"
```

### Issue: mTLS Between Services Failing
```bash
# Check if client certificate is valid
openssl verify -CAfile ca.crt client.crt

# Check Envoy configuration
kubectl describe configmap test-harborhook-envoy-config

# Look for TLS errors in logs
kubectl logs -l app.kubernetes.io/component=envoy | grep -i tls
kubectl logs -l app.kubernetes.io/component=ingest | grep -i tls

# Common issue: Certificate not mounted correctly
kubectl exec test-harborhook-envoy-xxx -- ls -la /etc/certs/
```

### Issue: Certificate Permissions
```bash
# Certificates should have restrictive permissions
# In container or local filesystem:
chmod 600 *.key  # Private keys
chmod 644 *.crt  # Certificates

# In Kubernetes, secrets are auto-mounted with correct permissions
```

## Certificate Generation Script Details

The `generate_certs.sh` script creates:

```bash
#!/bin/bash
# High-level overview - see actual script for details

# 1. Generate CA certificate (if not exists)
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -out ca.crt \
  -subj "/CN=Harborhook Root CA"

# 2. Generate server certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr \
  -subj "/CN=harborhook-envoy"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt -days 365 -sha256

# 3. Generate client certificate (for mTLS)
openssl genrsa -out client.key 4096
openssl req -new -key client.key -out client.csr \
  -subj "/CN=harborhook-client"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt -days 365 -sha256

# 4. Cleanup CSR files
rm *.csr
```

## Best Practices

1. **Certificate Validity**: Use 1-year validity for production (365 days)
2. **CA Validity**: CA can have longer validity (10 years), but rotate if compromised
3. **Rotation Frequency**: Rotate 30-60 days before expiry
4. **Key Strength**: Use 4096-bit RSA keys minimum
5. **Backup**: Always backup certificates before rotation
6. **Testing**: Test rotation in staging before production
7. **Monitoring**: Set up automated expiry monitoring
8. **Documentation**: Log all rotations in audit trail
9. **Access Control**: Limit who can perform certificate rotation
10. **Secure Storage**: Store private keys in Kubernetes Secrets or KMS

## Certificate Management Tools

Consider using cert-manager for Kubernetes:
```yaml
# Future enhancement: Use cert-manager for automatic rotation
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: harborhook-tls
spec:
  secretName: harborhook-certs
  issuerRef:
    name: harborhook-ca
    kind: ClusterIssuer
  dnsNames:
    - harborhook-envoy
    - localhost
  duration: 8760h  # 1 year
  renewBefore: 720h  # Renew 30 days before expiry
```

## References
- [OpenSSL Documentation](https://www.openssl.org/docs/)
- [TLS Best Practices (Mozilla)](https://wiki.mozilla.org/Security/Server_Side_TLS)
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [Envoy TLS Configuration](https://www.envoyproxy.io/docs/envoy/latest/configuration/listeners/network_filters/tls_inspector_filter)

## Rotation Schedule

| Rotation Date | Expiry Date | Certificate Type | Performed By | Notes |
|---------------|-------------|------------------|--------------|-------|
| 2024-11-09    | 2025-11-09  | Server/Client    | Initial      | First deployment |
| 2025-10-10    | 2026-10-10  | TBD             | TBD          | Planned rotation |

## Post-Rotation Checklist
- [ ] Certificates rotated successfully
- [ ] All validation tests passed
- [ ] Services restarted and healthy
- [ ] No TLS errors in logs
- [ ] Old certificates backed up securely
- [ ] Audit log updated
- [ ] Next rotation scheduled (calendar reminder)
- [ ] Monitoring alerts configured for expiry
- [ ] Runbook updated with lessons learned
