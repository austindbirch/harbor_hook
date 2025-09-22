# Harborhook Observability Demo & Testing

This directory contains scripts to demonstrate and validate the Phase 5 observability features of Harborhook.

## Scripts

### `demo.sh` - Comprehensive Observability Demo

**Purpose**: Generates high-volume traffic to showcase all observability features working together.

**What it demonstrates**:
- 📊 **Metrics**: High-volume traffic generation with configurable success/failure rates
- 🔍 **Distributed Tracing**: Correlated events across services with trace IDs
- 📝 **Logs**: Structured logging with correlation across the pipeline
- 🚨 **Alerting**: SLO burn rate alerts triggered by traffic patterns
- 📈 **Dashboards**: Real-time visualization in Grafana

**Usage**:
```bash
# Run with default settings (2 minutes, 10 RPS, 20% failure rate)
./scripts/observability/demo.sh

# Customize traffic parameters
TRAFFIC_DURATION=300 HIGH_TRAFFIC_RPS=25 FAILURE_PERCENTAGE=30 ./scripts/observability/demo.sh
```

**Environment Variables**:
- `TRAFFIC_DURATION`: Duration of traffic generation in seconds (default: 120)
- `HIGH_TRAFFIC_RPS`: Baseline requests per second (default: 10)
- `BURST_TRAFFIC_RPS`: Burst traffic RPS for alert triggering (default: 50)
- `FAILURE_PERCENTAGE`: Percentage of requests that should fail (default: 20)
- `TENANT_ID`: Tenant identifier for the demo (default: "observability_demo")

**Generated Traffic Patterns**:
- **Baseline Traffic**: Sustained traffic at configured RPS with mixed success/failure
- **Realistic Patterns**: Various event types (user_signup, order_placed, etc.) across multiple tenants
- **Trace Correlation**: Events with embedded trace/span IDs for distributed tracing
- **Alert Triggering**: High-failure burst traffic to trigger SLO burn rate alerts

### `e2e_test.sh` - End-to-End Observability Validation

**Purpose**: Validates that all observability components are properly configured and functioning.

**What it tests**:
- 🏥 **Service Health**: All observability services are running and healthy
- 📊 **Metrics Collection**: Prometheus is scraping Harborhook metrics
- 🚨 **Alert System**: Alert rules are loaded and AlertManager is configured
- 📝 **Log Aggregation**: Loki is collecting and indexing logs
- 🔍 **Distributed Tracing**: Tempo is accessible and configured for trace ingestion
- 📈 **Grafana Integration**: Data sources and dashboards are configured
- 🔄 **End-to-End Flow**: Complete event publishing → delivery → metrics workflow

**Usage**:
```bash
# Run all tests
./scripts/observability/e2e_test.sh

# Example output:
# 🧪 Harborhook Observability E2E Tests
# ==============================================
# 
# 🏥 Service Health Tests
# ==============================================
# 🧪 Testing Prometheus health endpoint
# ✅ PASS: Prometheus is healthy
# ...
# 
# 📋 Test Summary  
# ==============================================
# Total Tests: 25
# Passed: 25
# Failed: 0
# Pass Rate: 100.0%
# 🎉 All tests passed! Observability stack is fully functional.
```

## Prerequisites

Both scripts require:
- Harborhook services running (ingest, worker, jwks-server)
- Observability stack running (Prometheus, Grafana, Loki, Tempo, AlertManager)
- `harborctl` binary built (`make build-cli`)
- Dependencies: `curl`, `jq`, `bc`, `nc`

## Quick Start

1. **Start the full stack**:
   ```bash
   cd deploy/docker
   docker-compose up -d
   ```

2. **Build harborctl**:
   ```bash
   make build-cli
   ```

3. **Run the E2E tests** (recommended first step):
   ```bash
   ./scripts/observability/e2e_test.sh
   ```

4. **Run the observability demo**:
   ```bash
   ./scripts/observability/demo.sh
   ```

5. **Explore the results**:
   - **Grafana**: http://localhost:3000 (admin/admin)
   - **Prometheus**: http://localhost:9090
   - **AlertManager**: http://localhost:9093

## Observability Stack URLs

| Component | URL | Purpose |
|-----------|-----|---------|
| **Grafana** | http://localhost:3000 | Dashboards, visualization, data exploration |
| **Prometheus** | http://localhost:9090 | Metrics storage and querying |
| **AlertManager** | http://localhost:9093 | Alert management and notifications |
| **Tempo** | http://localhost:3200 | Distributed tracing (via Grafana) |
| **Loki** | http://localhost:3100 | Log aggregation (via Grafana) |
| **NSQ Admin** | http://localhost:4171 | Message queue monitoring |

## Expected Outcomes

### After Running the Demo

1. **Metrics Dashboard**: Grafana should show:
   - Event publishing rates
   - Delivery success/failure rates  
   - Latency histograms
   - Backlog depth over time

2. **Traces**: Tempo/Grafana should show:
   - Distributed traces across ingest → NSQ → worker → delivery
   - Correlated trace IDs in logs
   - Service dependency mapping

3. **Logs**: Loki/Grafana should show:
   - Structured logs from all services
   - Correlated events via trace IDs
   - Error patterns and debugging information

4. **Alerts**: Prometheus/AlertManager should show:
   - SLO burn rate alerts triggered during burst traffic
   - Alert state transitions (pending → firing → resolved)
   - Alert correlation with actual traffic patterns

### Troubleshooting

- **No metrics in Prometheus**: Check service discovery and scraping configuration
- **No traces in Tempo**: Verify OTLP endpoint configuration and trace export
- **No logs in Loki**: Check Promtail configuration and log shipping
- **Alerts not triggering**: Verify alert rule evaluation and metric thresholds
- **Tests failing**: Run individual service health checks and verify Docker containers are running

## Integration with Phase 5

These scripts validate the complete Phase 5 observability implementation:

- ✅ **Metrics**: Custom Harborhook metrics with business KPIs
- ✅ **Tracing**: OpenTelemetry instrumentation with correlation
- ✅ **Logging**: Structured logging with trace correlation  
- ✅ **Alerting**: SLO-based alerts with multi-window burn rates
- ✅ **Dashboards**: Pre-configured Grafana dashboards
- ✅ **Integration**: All three pillars working together with correlation