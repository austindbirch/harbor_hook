---
title: "High Latency Response"
weight: 30
---

# Runbook: High Delivery Latency

## Overview
This runbook addresses situations where webhook deliveries are taking significantly longer than expected, degrading customer experience and potentially violating SLAs.

## Severity
**Medium-High** - While deliveries may still succeed, high latency impacts customer workflows and can indicate deeper system issues.

## Symptoms
- Alert: `HighDeliveryLatency` or `P99LatencyHigh` firing
- Metric: `harborhook_delivery_latency_seconds` p95/p99 elevated
- Customer complaints about slow webhook delivery
- Grafana latency dashboard showing sustained increases
- Long-running traces in Tempo

## Impact
- Delayed webhook notifications to customers
- Potential timeout issues in customer systems
- Poor user experience and SLA violations
- Possible cascading effects (backlog growth)

## SLA Thresholds
- **Target**: p50 < 1s, p95 < 3s, p99 < 5s
- **Warning**: p95 > 5s, p99 > 10s
- **Critical**: p95 > 10s, p99 > 30s

## Investigation Steps

### 1. Confirm Latency Degradation
```bash
# Query current latency percentiles
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.95, rate(harborhook_delivery_latency_seconds_bucket[5m]))' | jq

curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99, rate(harborhook_delivery_latency_seconds_bucket[5m]))' | jq

# Compare to historical baseline
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.95, rate(harborhook_delivery_latency_seconds_bucket[5m] offset 1h))' | jq
```

### 2. Identify Which Component is Slow
Latency can be broken down into:
- **Ingest latency**: Time to publish event
- **Queue latency**: Time message sits in NSQ
- **Worker processing latency**: Time worker takes to deliver
- **Network latency**: Time for HTTP request to customer endpoint

```bash
# Check trace spans in Grafana Tempo
# Navigate to Grafana -> Explore -> Tempo
# Search for traces with tag: service.name=harborhook-worker
# Look for spans with long duration

# Query database for slow deliveries
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     d.id,
     ep.url,
     d.latency_ms,
     d.http_status,
     d.updated_at,
     e.event_type
   FROM harborhook.deliveries d
   JOIN harborhook.endpoints ep ON d.endpoint_id = ep.id
   JOIN harborhook.events e ON d.event_id = e.id
   WHERE d.latency_ms > 3000
     AND d.updated_at > NOW() - INTERVAL '30 minutes'
   ORDER BY d.latency_ms DESC
   LIMIT 20;"
```

### 3. Check for Slow Customer Endpoints
```bash
# Identify endpoints with consistently high latency
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     ep.url,
     ep.tenant_id,
     COUNT(*) as delivery_count,
     AVG(d.latency_ms) as avg_latency,
     PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY d.latency_ms) as p95_latency,
     MAX(d.latency_ms) as max_latency
   FROM harborhook.deliveries d
   JOIN harborhook.endpoints ep ON d.endpoint_id = ep.id
   WHERE d.updated_at > NOW() - INTERVAL '1 hour'
     AND d.status IN ('delivered', 'failed')
   GROUP BY ep.url, ep.tenant_id
   HAVING AVG(d.latency_ms) > 2000
   ORDER BY avg_latency DESC
   LIMIT 10;"

# Test endpoint directly
time curl -X POST https://slow-customer.example.com/webhook \
  -H "Content-Type: application/json" \
  -H "X-HarborHook-Signature: sha256=test" \
  -H "X-HarborHook-Timestamp: $(date +%s)" \
  -d '{"test": true}'
```

### 4. Check System Resources
```bash
# Worker resource utilization
kubectl top pods -l app.kubernetes.io/component=worker

# Node resource utilization
kubectl top nodes

# Database connection pool
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     state,
     COUNT(*)
   FROM pg_stat_activity
   WHERE datname = 'harborhook'
   GROUP BY state;"

# Check for network issues
kubectl logs -l app.kubernetes.io/component=worker --tail=100 | grep -i "connection\|timeout\|network"
```

### 5. Analyze Traces
```bash
# In Grafana Tempo UI:
# 1. Go to Explore -> Tempo
# 2. Search with filters:
#    - service.name=harborhook-worker
#    - duration > 3s
# 3. Examine span breakdown to find bottleneck:
#    - HTTP POST to customer endpoint
#    - Database queries
#    - NSQ message consumption
```

## Common Root Causes

### 1. **Slow Customer Endpoints**
- **Symptoms**: High latency concentrated on specific endpoints, HTTP timeouts
- **Root cause**: Customer webhook receiver is slow (processing, database queries, etc.)
- **Resolution**: Contact customer, implement endpoint-level rate limiting

### 2. **Worker Resource Exhaustion**
- **Symptoms**: Workers at 100% CPU/memory, latency across all endpoints
- **Root cause**: Insufficient worker capacity or resource limits too low
- **Resolution**: Scale workers or increase resource limits

### 3. **Database Slow Queries**
- **Symptoms**: Latency in delivery status updates, spans show slow DB queries
- **Root cause**: Missing indexes, lock contention, or query optimization needed
- **Resolution**: Optimize queries, add indexes, tune database

### 4. **Network Congestion**
- **Symptoms**: Timeouts, TCP retransmissions, DNS resolution failures
- **Root cause**: Network path to customer endpoints is slow
- **Resolution**: Check network path, consider regional workers/CDN

### 5. **NSQ Queue Depth**
- **Symptoms**: Messages sitting in queue for extended periods
- **Root cause**: Worker throughput < message arrival rate
- **Resolution**: Scale workers (see backlog-growth runbook)

### 6. **Retry Delays**
- **Symptoms**: Latency increases during retry attempts
- **Root cause**: Exponential backoff adding significant delays
- **Resolution**: Expected behavior, but consider tuning backoff schedule

## Remediation Steps

### Option 1: Identify and Rate-Limit Slow Endpoints
```bash
# From investigation step 3, get slow endpoint IDs

# Set rate limit to prevent one slow endpoint from affecting others
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "UPDATE harborhook.endpoints
   SET rate_per_sec = 5
   WHERE url = 'https://slow-customer.example.com/webhook';"

# Contact customer
# Subject: Webhook Endpoint Performance Issue
# Body: Your endpoint at [URL] is responding slowly (avg latency: Xms).
#       This may impact other customers. Please optimize or contact us.
```

### Option 2: Reduce Worker Timeout
```bash
# If many endpoints are timing out, consider reducing timeout

# Edit worker ConfigMap
kubectl edit configmap test-harborhook-worker-config

# Change HTTP_CLIENT_TIMEOUT from "30s" to "15s"
# This will fail slow requests faster, preventing worker tie-up

# Restart workers to apply
kubectl rollout restart deployment test-harborhook-worker

# Monitor impact on success rate
curl -s 'http://localhost:9090/api/v1/query?query=rate(harborhook_deliveries_total{status="failed"}[5m])' | jq
```

### Option 3: Scale Workers
```bash
# If workers are resource-constrained, scale up
kubectl scale deployment test-harborhook-worker --replicas=6

# Or increase resource limits
kubectl edit deployment test-harborhook-worker
# Increase CPU/memory limits under resources section

# Verify improvement
kubectl top pods -l app.kubernetes.io/component=worker
```

### Option 4: Optimize Database
```bash
# Add missing indexes if identified
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_deliveries_updated_at
   ON harborhook.deliveries(updated_at DESC);"

# Check for slow queries
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     query,
     mean_exec_time,
     calls
   FROM pg_stat_statements
   WHERE query LIKE '%harborhook%'
   ORDER BY mean_exec_time DESC
   LIMIT 10;"

# Vacuum if needed
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "VACUUM ANALYZE harborhook.deliveries;"
```

### Option 5: Implement Async Patterns
For persistent latency issues:
```go
// Consider implementing async delivery confirmation
// Instead of waiting for HTTP response, accept webhook and confirm later
// This requires architectural changes but eliminates customer endpoint latency impact
```

## Prevention

### 1. Endpoint Health Scoring
```python
# Implement automatic endpoint health tracking
# Score endpoints based on:
# - Average latency (lower is better)
# - Success rate (higher is better)
# - Consistency (less variance is better)

# Auto-throttle endpoints with poor health scores
if endpoint_health_score < 0.5:
    set_rate_limit(endpoint_id, 5)  # Limit to 5/sec
    notify_customer(endpoint_id)
```

### 2. Proactive Monitoring
```yaml
# Alert on latency trends before they become critical
- alert: DeliveryLatencyIncreasing
  expr: |
    histogram_quantile(0.95, rate(harborhook_delivery_latency_seconds_bucket[5m]))
    >
    histogram_quantile(0.95, rate(harborhook_delivery_latency_seconds_bucket[5m] offset 1h)) * 1.5
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: "Delivery latency increasing by 50% over 1h ago"
    description: "P95 latency now {{ $value }}s, investigate before it escalates"
```

### 3. SLO Dashboard
Create customer-facing dashboard showing:
- Current delivery latency (p50, p95, p99)
- Success rate
- Endpoint-specific metrics
- Historical trends

### 4. Capacity Planning
- Monitor typical latency patterns
- Set worker count to maintain p95 < 3s under typical load
- Review and test during peak hours
- Document expected throughput and latency per worker

### 5. Customer Best Practices Documentation
Provide guidance to customers on:
- Webhook receiver performance (should respond in <1s)
- Async processing (accept webhook, process later)
- Timeout handling
- Retry expectations

## Rollback Procedures
If remediation causes issues:
```bash
# Revert timeout changes
kubectl edit configmap test-harborhook-worker-config
# Change HTTP_CLIENT_TIMEOUT back to original value
kubectl rollout restart deployment test-harborhook-worker

# Revert rate limits
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "UPDATE harborhook.endpoints SET rate_per_sec = 0 WHERE id = 'ENDPOINT_ID';"

# Scale workers back down if that caused issues
kubectl scale deployment test-harborhook-worker --replicas=3
```

## Validation Steps
```bash
# 1. Verify latency is decreasing
watch -n 30 'curl -s "http://localhost:9090/api/v1/query?query=histogram_quantile(0.95, rate(harborhook_delivery_latency_seconds_bucket[5m]))" | jq .data.result[0].value[1]'

# 2. Check success rate hasn't degraded
curl -s 'http://localhost:9090/api/v1/query?query=rate(harborhook_deliveries_total{status="delivered"}[5m]) / rate(harborhook_deliveries_total[5m])' | jq

# 3. Review recent deliveries
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     AVG(latency_ms) as avg_latency,
     PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY latency_ms) as p95_latency,
     COUNT(*) as count
   FROM harborhook.deliveries
   WHERE updated_at > NOW() - INTERVAL '10 minutes';"

# 4. Check Grafana dashboards
# Open latency dashboard and verify improvement

# 5. Spot-check slow endpoints
# Verify they're now rate-limited or improved
```

## Diagnostic Queries

### Find Current Slowest Endpoints
```sql
SELECT
  ep.url,
  ep.tenant_id,
  COUNT(*) as recent_deliveries,
  AVG(d.latency_ms) as avg_latency_ms,
  MAX(d.latency_ms) as max_latency_ms
FROM harborhook.deliveries d
JOIN harborhook.endpoints ep ON d.endpoint_id = ep.id
WHERE d.updated_at > NOW() - INTERVAL '15 minutes'
  AND d.latency_ms IS NOT NULL
GROUP BY ep.url, ep.tenant_id
ORDER BY avg_latency_ms DESC
LIMIT 10;
```

### Latency Distribution
```sql
SELECT
  CASE
    WHEN latency_ms < 100 THEN '<100ms'
    WHEN latency_ms < 500 THEN '100-500ms'
    WHEN latency_ms < 1000 THEN '500ms-1s'
    WHEN latency_ms < 3000 THEN '1-3s'
    WHEN latency_ms < 10000 THEN '3-10s'
    ELSE '>10s'
  END as latency_bucket,
  COUNT(*) as count,
  ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2) as percentage
FROM harborhook.deliveries
WHERE updated_at > NOW() - INTERVAL '1 hour'
  AND latency_ms IS NOT NULL
GROUP BY latency_bucket
ORDER BY MIN(latency_ms);
```

## References
- [Worker Configuration](../../charts/harborhook/values.yaml)
- [Customer Webhook Best Practices](../webhook-best-practices.md)
- [Backlog Growth Runbook](./backlog-growth.md)
- [Tempo Tracing Guide](../observability/tracing.md)

## Post-Incident Checklist
- [ ] Root cause identified and documented
- [ ] Slow endpoints identified and addressed
- [ ] Worker capacity reviewed and adjusted
- [ ] Customer notifications sent (if applicable)
- [ ] SLO/alert thresholds reviewed
- [ ] Dashboards updated with new insights
- [ ] Customer best practices documentation reviewed
- [ ] Runbook updated with lessons learned
