# Runbook: DLQ Spike

## Overview
This runbook covers how to respond to a spike in Dead Letter Queue (DLQ) entries, indicating a significant number of webhook deliveries have permanently failed after exhausting all retry attempts.

## Severity
**High** - Indicates customers are not receiving webhook notifications, potentially affecting critical business workflows.

## Symptoms
- Alert: `DLQHighRate` firing (DLQ entries increasing rapidly)
- Metric: `harborhook_dlq_total` showing significant growth
- Customer reports: Missing webhook deliveries
- Grafana DLQ dashboard showing spike in failed deliveries

## Impact
- Webhook notifications not reaching customer endpoints
- Potential data loss if deliveries cannot be replayed
- Customer trust and SLA violations
- Increased operational load from support tickets

## Investigation Steps

### 1. Confirm the Spike
```bash
# Check current DLQ count in Prometheus
curl -s 'http://localhost:9090/api/v1/query?query=harborhook_dlq_total' | jq '.data.result[0].value[1]'

# Or in Kubernetes
kubectl port-forward svc/prometheus 9090:9090
# Then access http://localhost:9090 and run query: harborhook_dlq_total
```

### 2. Identify Affected Endpoints
```bash
# Query DLQ entries from database
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     e.tenant_id,
     ep.url,
     COUNT(*) as dlq_count,
     MAX(d.updated_at) as last_failure
   FROM harborhook.dlq dlq
   JOIN harborhook.deliveries d ON dlq.delivery_id = d.id
   JOIN harborhook.endpoints ep ON d.endpoint_id = ep.id
   JOIN harborhook.events e ON d.event_id = e.id
   WHERE dlq.created_at > NOW() - INTERVAL '1 hour'
   GROUP BY e.tenant_id, ep.url
   ORDER BY dlq_count DESC
   LIMIT 10;"

# Or using harborctl
./bin/harborctl delivery dlq --limit 50 --server localhost:8443
```

### 3. Analyze Failure Patterns
```bash
# Check error reasons in DLQ
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     reason,
     COUNT(*) as count
   FROM harborhook.dlq
   WHERE created_at > NOW() - INTERVAL '1 hour'
   GROUP BY reason
   ORDER BY count DESC;"

# Check HTTP status codes from failed deliveries
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     d.http_status,
     d.last_error,
     COUNT(*) as count
   FROM harborhook.deliveries d
   WHERE d.status = 'dead'
     AND d.updated_at > NOW() - INTERVAL '1 hour'
   GROUP BY d.http_status, d.last_error
   ORDER BY count DESC
   LIMIT 10;"
```

### 4. Check Endpoint Health
```bash
# Test endpoint connectivity manually
curl -v -X POST https://customer-endpoint.example.com/webhook \
  -H "Content-Type: application/json" \
  -d '{"test": true}'

# Check if endpoint is returning consistent errors
# Review worker logs for the specific endpoint
kubectl logs -l app.kubernetes.io/component=worker --tail=100 | grep "endpoint_id"
```

## Common Root Causes

### 1. **Customer Endpoint Down**
- **Symptoms**: All deliveries to specific endpoint(s) failing with connection timeouts or 5xx errors
- **Root cause**: Customer's webhook receiver is down or unreachable
- **Resolution**: Contact customer to fix their endpoint

### 2. **Authentication/Authorization Issues**
- **Symptoms**: Consistent 401/403 errors
- **Root cause**: Customer changed their endpoint security without updating configuration
- **Resolution**: Customer needs to update endpoint credentials or whitelist Harborhook IPs

### 3. **Invalid Endpoint URL**
- **Symptoms**: DNS resolution failures or connection refused
- **Root cause**: Endpoint URL is misconfigured or no longer valid
- **Resolution**: Customer needs to update endpoint URL

### 4. **Payload Schema Issues**
- **Symptoms**: 400/422 errors from customer endpoint
- **Root cause**: Webhook payload format changed or customer validation rules changed
- **Resolution**: Investigate payload format changes, coordinate with customer

### 5. **Rate Limiting**
- **Symptoms**: 429 errors from customer endpoint
- **Root cause**: Sending too many webhooks too fast for customer's rate limits
- **Resolution**: Adjust delivery rate for the endpoint

## Remediation Steps

### Option 1: Contact Customer (Most Common)
```bash
# Get customer contact info
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT tenant_id, url FROM harborhook.endpoints WHERE id = 'ENDPOINT_ID';"

# Notify customer via support ticket or email
# Provide them with:
# - Failure count and timeframe
# - Sample error messages
# - Request to verify endpoint health
```

### Option 2: Replay Failed Deliveries (After Fix)
```bash
# Once customer confirms their endpoint is fixed, replay DLQ entries
./bin/harborctl delivery replay-dlq --tenant-id tn_customer --limit 100 --server localhost:8443

# Monitor replay progress
watch -n 5 './bin/harborctl delivery dlq --limit 10 --server localhost:8443'
```

### Option 3: Disable Problematic Endpoint (Emergency)
```bash
# If endpoint is causing cascading failures, temporarily disable it
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "UPDATE harborhook.endpoints
   SET rate_per_sec = 0
   WHERE id = 'ENDPOINT_ID';"

# Document the disable action
echo "Endpoint ENDPOINT_ID disabled at $(date) due to excessive failures" >> /var/log/harborhook/emergency-actions.log

# Notify customer immediately
```

### Option 4: Clear DLQ (Data Loss - Last Resort)
```bash
# Only if deliveries are truly unrecoverable and bloating the database
# THIS WILL PERMANENTLY DELETE DATA - GET APPROVAL FIRST

# Backup DLQ data first
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres pg_dump -U postgres -d harborhook \
  -t harborhook.dlq -t harborhook.deliveries > dlq_backup_$(date +%Y%m%d_%H%M%S).sql

# Delete old DLQ entries (older than 30 days)
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "DELETE FROM harborhook.dlq WHERE created_at < NOW() - INTERVAL '30 days';"
```

## Prevention

### 1. Endpoint Validation
- Implement endpoint health checks before accepting subscriptions
- Periodically ping endpoints to detect issues early
- Set up alerting for endpoint-level failure rates

### 2. Circuit Breakers
- Implement circuit breaker pattern for consistently failing endpoints
- Auto-disable endpoints that exceed failure threshold
- Notify customers proactively when endpoints are degraded

### 3. Better Customer Communication
- Provide webhook status dashboard for customers
- Send proactive alerts when endpoint failure rate increases
- Document best practices for webhook receivers

### 4. Graceful Degradation
- Implement exponential backoff with longer delays
- Consider adaptive retry strategies based on error type
- Set up separate DLQ handling for transient vs permanent failures

### 5. Monitoring and Alerting
```yaml
# Alert rule for early detection
- alert: DLQGrowthRate
  expr: rate(harborhook_dlq_total[5m]) > 10
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "DLQ entries increasing at {{ $value }} entries/sec"
    description: "Investigate failing endpoints before situation escalates"
```

## Rollback Procedures
N/A - DLQ issues typically require forward fixes (endpoint repairs, replays).

## Communication Templates

### Customer Notification Email
```
Subject: [Action Required] Webhook Delivery Failures to Your Endpoint

Hi [Customer],

We've detected that webhook deliveries to your endpoint have been failing consistently:

Endpoint: [URL]
Failure Count: [COUNT]
Time Period: [TIMEFRAME]
Error Pattern: [ERROR SUMMARY]

Action needed:
1. Verify your webhook endpoint is accessible and healthy
2. Check for any recent configuration changes
3. Review our signature verification requirements
4. Contact support if you need assistance

Once confirmed fixed, we can replay the failed webhooks.

Thanks,
Harborhook Operations
```

## References
- [Webhook Signature Verification Guide](../signature-verification.md)
- [Harborctl Replay Commands](../harborctl.md)
- [Delivery Status API](../api/deliveries.md)
- [NSQ Management Commands](../../CLAUDE.md#troubleshooting)

## Post-Incident Review Checklist
- [ ] Root cause identified and documented
- [ ] Affected customers notified
- [ ] Failed deliveries replayed successfully
- [ ] Prevention measures implemented
- [ ] Monitoring/alerting updated if needed
- [ ] Runbook updated with lessons learned
