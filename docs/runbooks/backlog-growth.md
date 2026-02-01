---
title: "Backlog Growth Response"
weight: 20
---

# Runbook: NSQ Backlog Growth

## Overview
This runbook addresses situations where the NSQ delivery queue is growing faster than workers can process it, leading to increased delivery latency and potential system instability.

## Severity
**High** - Can lead to delayed webhook deliveries, customer complaints, and eventual system resource exhaustion if not addressed.

## Symptoms
- Alert: `NSQBacklogHigh` or `WorkerBacklogGrowing` firing
- Metric: `harborhook_worker_backlog` increasing steadily
- NSQ admin UI showing large message depth in `deliveries` topic
- Customers reporting delayed webhook notifications
- Worker CPU/memory usage remaining normal despite backlog

## Impact
- Increased delivery latency (webhooks delayed by minutes/hours)
- Potential event ordering issues for customers
- Risk of NSQ memory exhaustion
- Degraded user experience

## Investigation Steps

### 1. Confirm Backlog Growth
```bash
# Check current backlog via Prometheus
curl -s 'http://localhost:9090/api/v1/query?query=harborhook_worker_backlog' | jq

# Check NSQ stats directly
curl -s http://localhost:4171/api/topics/deliveries | jq '.depth'

# Or in Kubernetes
kubectl port-forward svc/test-harborhook-nsqadmin 4171:4171
# Then visit http://localhost:4171
```

### 2. Check Worker Health and Capacity
```bash
# Check worker pod status
kubectl get pods -l app.kubernetes.io/component=worker

# Check worker resource usage
kubectl top pods -l app.kubernetes.io/component=worker

# Review worker logs for errors or slowness
kubectl logs -l app.kubernetes.io/component=worker --tail=100

# Check worker replica count
kubectl get deployment -l app.kubernetes.io/component=worker
```

### 3. Identify Throughput Bottleneck
```bash
# Check delivery rate over time
curl -s 'http://localhost:9090/api/v1/query?query=rate(harborhook_deliveries_total[5m])' | jq

# Check event publishing rate
curl -s 'http://localhost:9090/api/v1/query?query=rate(harborhook_events_published_total[5m])' | jq

# Calculate the gap
# If publish rate > delivery rate for extended period = backlog will grow
```

### 4. Check for Slow Endpoints
```bash
# Find endpoints with high latency
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "SELECT
     ep.url,
     ep.tenant_id,
     COUNT(*) as delivery_count,
     AVG(d.latency_ms) as avg_latency,
     MAX(d.latency_ms) as max_latency
   FROM harborhook.deliveries d
   JOIN harborhook.endpoints ep ON d.endpoint_id = ep.id
   WHERE d.updated_at > NOW() - INTERVAL '30 minutes'
   GROUP BY ep.url, ep.tenant_id
   ORDER BY avg_latency DESC
   LIMIT 10;"

# Check for hanging requests in worker logs
kubectl logs -l app.kubernetes.io/component=worker --tail=500 | grep -i "timeout\|slow\|hanging"
```

### 5. Check NSQ Channel Health
```bash
# Check if worker channel is paused or has issues
curl -s http://localhost:4151/stats | jq '.topics[] | select(.topic_name=="deliveries")'

# Check for in-flight message count (should be processing)
curl -s http://localhost:4151/stats | jq '.topics[] | select(.topic_name=="deliveries") | .channels[] | select(.channel_name=="workers") | .in_flight_count'
```

## Common Root Causes

### 1. **Insufficient Worker Capacity**
- **Symptoms**: Worker pods at 100% CPU, backlog growing during peak hours
- **Root cause**: Not enough worker replicas to handle load
- **Resolution**: Scale up workers

### 2. **Slow Customer Endpoints**
- **Symptoms**: High average latency, workers waiting on slow HTTP responses
- **Root cause**: Customer endpoints taking >5s to respond
- **Resolution**: Identify and rate-limit slow endpoints, reduce timeout

### 3. **Event Publishing Spike**
- **Symptoms**: Sudden increase in `harborhook_events_published_total`
- **Root cause**: Customer traffic spike, bulk operations, or runaway publisher
- **Resolution**: May be expected; scale workers or investigate if anomalous

### 4. **Worker Crashes/Restarts**
- **Symptoms**: Worker pods in CrashLoopBackoff or frequently restarting
- **Root cause**: Bugs, OOM, configuration issues
- **Resolution**: Fix underlying issue, review logs

### 5. **Message Requeue Loop**
- **Symptoms**: Same messages repeatedly failing and requeuing
- **Root cause**: Retry logic bug, malformed messages, or endpoint always failing
- **Resolution**: Identify stuck messages and move to DLQ

### 6. **NSQ Memory Pressure**
- **Symptoms**: NSQ pod memory at limit, message processing slows
- **Root cause**: Backlog too large for available memory
- **Resolution**: Scale NSQ, clear backlog, increase limits

## Remediation Steps

### Option 1: Scale Up Workers (Most Common)
```bash
# In Kubernetes - scale worker deployment
kubectl scale deployment test-harborhook-worker --replicas=6

# Verify scaling
kubectl get pods -l app.kubernetes.io/component=worker

# Monitor backlog reduction
watch -n 5 'curl -s http://localhost:4171/api/topics/deliveries | jq .depth'

# In Docker Compose
cd deploy/docker
docker-compose up -d --scale worker=6
```

### Option 2: Increase Worker Concurrency
```bash
# Edit worker configuration to handle more concurrent deliveries
kubectl edit configmap test-harborhook-worker-config

# Update WORKER_CONCURRENCY from 100 to 200
# Save and restart workers
kubectl rollout restart deployment test-harborhook-worker
```

### Option 3: Identify and Throttle Slow Endpoints
```bash
# Find slowest endpoints from investigation step 4

# Set rate limit for slow endpoint
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c \
  "UPDATE harborhook.endpoints
   SET rate_per_sec = 5
   WHERE url = 'https://slow-customer.example.com/webhook';"

# Or reduce worker timeout (edit worker config)
# HTTP_CLIENT_TIMEOUT: "15s" -> "10s"
```

### Option 4: Clear Stuck Messages (Last Resort)
```bash
# Check for old messages stuck in the queue
curl -s http://localhost:4151/stats | jq '.topics[] | select(.topic_name=="deliveries") | .channels[] | select(.channel_name=="workers")'

# If messages are truly stuck (same in_flight_count for hours)
# Empty the channel (WARNING: Messages will be lost)
curl -X POST http://localhost:4171/api/topics/deliveries/channel/workers/empty

# Better: Pause, inspect, then manually process or move to DLQ
curl -X POST http://localhost:4171/api/topics/deliveries/channel/workers/pause

# After investigation, unpause
curl -X POST http://localhost:4171/api/topics/deliveries/channel/workers/unpause
```

### Option 5: Emergency - Drain Backlog Gradually
```bash
# If backlog is massive (millions of messages), drain it safely

# Scale workers aggressively but monitor resources
kubectl scale deployment test-harborhook-worker --replicas=20

# Monitor system health
kubectl top nodes
kubectl top pods

# Watch for OOM, network saturation, database connection exhaustion
# Scale back if system becomes unstable

# Once backlog is under control, scale back to normal
kubectl scale deployment test-harborhook-worker --replicas=3
```

## Prevention

### 1. Auto-Scaling
```yaml
# Horizontal Pod Autoscaler for workers
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: worker-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: harborhook-worker
  minReplicas: 3
  maxReplicas: 20
  metrics:
  - type: Pods
    pods:
      metric:
        name: harborhook_worker_backlog
      target:
        type: AverageValue
        averageValue: "1000"
```

### 2. Proactive Monitoring
```yaml
# Alert on sustained backlog growth (before it becomes critical)
- alert: WorkerBacklogGrowing
  expr: |
    (harborhook_worker_backlog - harborhook_worker_backlog offset 5m) > 1000
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "Worker backlog growing by {{ $value }} messages"
    description: "Consider scaling workers or investigating slow endpoints"
```

### 3. Capacity Planning
- Monitor typical message rates and worker throughput
- Set worker replica count to handle 2x typical peak load
- Document expected throughput per worker (e.g., 100 deliveries/sec per worker)
- Review capacity quarterly or after major customer onboarding

### 4. Circuit Breakers for Slow Endpoints
- Implement automatic detection of slow endpoints (p95 > 10s)
- Auto-throttle or temporarily skip slow endpoints
- Alert customer when their endpoint is causing backlog

### 5. Message TTL
- Consider implementing message expiry for old events (e.g., >24 hours)
- Auto-move to DLQ if message age exceeds threshold
- Prevents ancient messages from clogging the queue

## Rollback Procedures
If scaling workers causes issues:
```bash
# Scale back to previous replica count
kubectl scale deployment test-harborhook-worker --replicas=3

# If configuration changes caused issues
kubectl rollout undo deployment test-harborhook-worker

# Verify rollback
kubectl rollout status deployment test-harborhook-worker
```

## Validation Steps
After remediation:
```bash
# 1. Verify backlog is decreasing
watch -n 10 'curl -s http://localhost:4171/api/topics/deliveries | jq .depth'

# 2. Check delivery rate is healthy
curl -s 'http://localhost:9090/api/v1/query?query=rate(harborhook_deliveries_total[5m])' | jq

# 3. Verify worker health
kubectl get pods -l app.kubernetes.io/component=worker
kubectl top pods -l app.kubernetes.io/component=worker

# 4. Check for new errors
kubectl logs -l app.kubernetes.io/component=worker --tail=50 | grep ERROR

# 5. Monitor customer-facing metrics
# - Delivery latency should be decreasing
# - Success rate should remain stable
```

## Performance Benchmarks
- **Normal state**: Backlog < 1000 messages, latency < 5s
- **Warning state**: Backlog 1000-10000 messages, latency 5-30s
- **Critical state**: Backlog > 10000 messages, latency > 60s

## References
- [NSQ Troubleshooting Commands](../../CLAUDE.md#nsq-management)
- [Worker Configuration](../../charts/harborhook/values.yaml)
- [Scaling Documentation](../architecture.md#scaling)
- [DLQ Spike Runbook](./dlq-spike.md) (related issue)

## Post-Incident Checklist
- [ ] Root cause identified
- [ ] Worker capacity adjusted appropriately
- [ ] Auto-scaling configured (if not already)
- [ ] Slow endpoints identified and addressed
- [ ] Monitoring/alerting thresholds reviewed
- [ ] Capacity plan updated
- [ ] Runbook updated with lessons learned
