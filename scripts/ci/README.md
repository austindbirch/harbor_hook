# CI Scripts

This directory contains scripts specifically designed to run in the CI/CD pipeline environment.

## e2e_test.sh

End-to-end test script for validating Harborhook functionality in Kubernetes (KinD cluster).

### What it tests

1. **Service Health**: Verifies all pods and services are running
2. **Database**: Checks PostgreSQL connectivity and schema
3. **NSQ**: Validates message queue is accessible
4. **End-to-End Workflow**:
   - Obtains JWT authentication token
   - Creates a webhook endpoint
   - Creates an event subscription
   - Publishes a test event
   - Verifies the event is delivered
   - Checks delivery status

### Requirements

- `kubectl` (configured to access the cluster)
- `curl`
- `jq`
- `bash` 4.0+

### Usage

```bash
# Set the Helm release name (default: test)
export RELEASE_NAME=test

# Run the tests
./scripts/ci/e2e_test.sh
```

### Environment Variables

- `RELEASE_NAME`: The Helm release name (default: `test`)

### Exit Codes

- `0`: All tests passed
- `1`: One or more tests failed

### How it works

The script uses `kubectl port-forward` to access services running in the Kubernetes cluster. It then performs HTTP requests against the Harborhook API through the Envoy gateway to verify the complete webhook delivery pipeline works correctly.

### Differences from `scripts/observability/e2e_test.sh`

The observability e2e script (`scripts/observability/e2e_test.sh`) is designed for local Docker Compose environments and tests the full observability stack (Prometheus, Grafana, Loki, Tempo, etc.).

This CI version focuses on core Harborhook functionality without the observability stack, using Kubernetes service discovery instead of localhost URLs.
