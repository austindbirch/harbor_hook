# Harborhook Operational Runbooks

This directory contains operational runbooks for managing and troubleshooting Harborhook in production.

## Overview

Runbooks provide step-by-step procedures for responding to common operational scenarios, including incidents, maintenance tasks, and security operations. Each runbook follows a standardized format with investigation steps, remediation procedures, and validation checklists.

## Available Runbooks

### Incident Response

#### [DLQ Spike](./dlq-spike.md)
**When to use**: High rate of failed webhook deliveries ending up in Dead Letter Queue

**Severity**: High

**Common scenarios**:
- Customer endpoints down or unreachable
- Authentication/authorization failures
- Invalid payload formats
- Rate limiting by customer endpoints

**Quick actions**:
```bash
# Check DLQ count
curl -s 'http://localhost:9090/api/v1/query?query=harborhook_dlq_total' | jq

# View recent DLQ entries
./bin/harborctl delivery dlq --limit 50 --server localhost:8443
```

---

#### [Backlog Growth](./backlog-growth.md)
**When to use**: NSQ delivery queue growing faster than workers can process

**Severity**: High

**Common scenarios**:
- Insufficient worker capacity
- Slow customer endpoints
- Event publishing spike
- Worker crashes or restarts

**Quick actions**:
```bash
# Check current backlog
curl -s http://localhost:4171/api/topics/deliveries | jq .depth

# Scale workers
kubectl scale deployment test-harborhook-worker --replicas=6
```

---

#### [High Latency](./high-latency.md)
**When to use**: Webhook delivery latency exceeds SLA thresholds

**Severity**: Medium-High

**Common scenarios**:
- Slow customer endpoints
- Worker resource exhaustion
- Database slow queries
- Network congestion

**Quick actions**:
```bash
# Check current latency
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.95, rate(harborhook_delivery_latency_seconds_bucket[5m]))' | jq

# Find slow endpoints
# See runbook for SQL query
```

---

### Maintenance Operations

#### [JWT Key Rotation](./jwt-rotation.md)
**When to use**: Scheduled key rotation (every 90 days) or emergency rotation after key compromise

**Severity**: Medium (scheduled) / High (emergency)

**Duration**: 90 minutes (scheduled) / 15 minutes (emergency)

**Overview**:
- Generate new RSA keypair
- Update jwks-server configuration
- Overlap period for zero-downtime rotation
- Verify and remove old keys

**Quick start**:
```bash
# Check certificate expiry
openssl x509 -in jwt-private-current.pem -noout -enddate

# Generate new keypair
openssl genrsa -out jwt-private-new.pem 4096

# See runbook for full procedure
```

---

#### [TLS Certificate Rotation](./cert-rotation.md)
**When to use**: 30 days before certificate expiry or emergency rotation after compromise

**Severity**: High (expired certs = outage)

**Duration**: 30 minutes

**Overview**:
- Generate new CA and certificates
- Update Kubernetes secrets or Docker volumes
- Rolling restart of services
- Verify HTTPS and mTLS functionality

**Quick start**:
```bash
# Check certificate expiry
openssl x509 -in server.crt -noout -enddate

# Generate new certificates
cd deploy/docker/envoy/certs
./generate_certs.sh

# See runbook for full procedure
```

---

## Runbook Standards

All runbooks follow this structure:

1. **Overview**: Brief description of the scenario
2. **Severity**: Impact level (Low/Medium/High/Critical)
3. **Symptoms**: How to recognize the issue
4. **Impact**: Business and technical consequences
5. **Investigation Steps**: Diagnostic commands and queries
6. **Common Root Causes**: Typical reasons for the issue
7. **Remediation Steps**: Step-by-step fix procedures
8. **Prevention**: Long-term solutions and improvements
9. **Rollback Procedures**: How to revert changes if needed
10. **Validation Checklist**: Tests to confirm resolution
11. **References**: Links to related documentation

## Using Runbooks

### During an Incident

1. **Identify the scenario** using symptoms and alerts
2. **Follow investigation steps** to diagnose root cause
3. **Execute remediation** following step-by-step instructions
4. **Validate the fix** using provided checklist
5. **Document the incident** with lessons learned
6. **Update the runbook** if new insights discovered

### For Scheduled Maintenance

1. **Schedule during maintenance window** (low traffic period)
2. **Notify stakeholders** of planned maintenance
3. **Prepare backup and rollback plan** before starting
4. **Follow the procedure** step by step
5. **Validate thoroughly** before completing
6. **Document completion** and schedule next occurrence

## Alerting Integration

Runbooks are linked to Prometheus alerts:

| Alert | Runbook | Severity |
|-------|---------|----------|
| `DLQHighRate` | [dlq-spike.md](./dlq-spike.md) | High |
| `NSQBacklogHigh` | [backlog-growth.md](./backlog-growth.md) | High |
| `WorkerBacklogGrowing` | [backlog-growth.md](./backlog-growth.md) | Warning |
| `HighDeliveryLatency` | [high-latency.md](./high-latency.md) | Medium |
| `P99LatencyHigh` | [high-latency.md](./high-latency.md) | Medium |
| `CertificateExpiringSoon` | [cert-rotation.md](./cert-rotation.md) | Warning |
| `CertificateExpiredOrExpiringSoon` | [cert-rotation.md](./cert-rotation.md) | Critical |

## Quick Reference Commands

### Health Checks
```bash
# All services
kubectl get pods

# NSQ backlog
curl -s http://localhost:4171/api/topics/deliveries | jq .depth

# DLQ count
./bin/harborctl delivery dlq --limit 10 --server localhost:8443

# Current latency (p95)
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.95, rate(harborhook_delivery_latency_seconds_bucket[5m]))' | jq
```

### Common Troubleshooting
```bash
# View worker logs
kubectl logs -l app.kubernetes.io/component=worker --tail=100

# Scale workers
kubectl scale deployment test-harborhook-worker --replicas=6

# Restart service
kubectl rollout restart deployment test-harborhook-worker

# Database query
kubectl exec test-postgres-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c "SELECT COUNT(*) FROM harborhook.deliveries WHERE status='dead';"
```

### Emergency Contacts

| Role | Responsibility | Contact Method |
|------|---------------|----------------|
| On-call Engineer | First responder | PagerDuty |
| Platform Lead | Escalation point | Slack/Phone |
| Database Admin | DB performance issues | Slack |
| Security Team | Security incidents | Slack/Email |

## Contributing to Runbooks

When updating runbooks:

1. **Test procedures** in staging before documenting
2. **Use specific commands** with actual paths and names
3. **Include expected output** for validation steps
4. **Add timestamps** to all changes
5. **Keep it actionable** - every step should be clear
6. **Update after incidents** with lessons learned

### Runbook Template

See [runbook-template.md](./runbook-template.md) for creating new runbooks.

## Related Documentation

- [Architecture Overview](../architecture.md)
- [Harborctl CLI Reference](../harborctl.md)
- [Monitoring and Alerting](../observability/)
- [Deployment Guide](../../charts/harborhook/README.md)
- [Troubleshooting](../../CLAUDE.md#troubleshooting)

## Feedback

Found an issue with a runbook or have suggestions for improvements?

- Open an issue: [GitHub Issues](https://github.com/austindbirch/harbor_hook/issues)
- Submit a PR with improvements
- Document lessons learned after incidents

## Version History

| Version | Date | Changes | Author |
|---------|------|---------|--------|
| 1.0.0 | 2025-11-09 | Initial runbooks created | Initial setup |

---

**Remember**: These runbooks are living documents. Update them after every incident and maintenance operation to keep them accurate and useful.
