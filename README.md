# HarborHook
A Go-first, multi-tenant, reliable webhook delivery platform that showcases platform engineering, systems design, SRE practice, and modern observability practices. 

## Quickstart (Kubernetes)

This project uses KinD for local dev.

```bash
# Bring up the entire stack in a local KinD cluster
make kind-up-and-test

# When you're done, tear down the cluster
make kind-down
```

## Docker Compose (Legacy)

The original Docker Compose setup is still available for reference.

```bash
# Generate protos (requires protoc-gen-go, -grpc, -grpc-gateway, -openapi)
make proto
# Build go binaries
make build
# Start stack
make up
# wait ~5-10s
make logs
# Stop stack
make down
# Prune Docker
make clean
```

## TODO
- [ ] Regardless of whether this is SaaS or a dev package, I need to add a way for users/devs to generate, store, and inject secrets (for event signing)

## Smoke checks

After running `make kind-up-and-test`, you can access the services via `kubectl port-forward`:
```bash
kubectl port-forward svc/harborhook-nsq-nsqadmin 4171:4171
```

## Legacy Checks
```bash
curl -s localhost:8080/healthz | jq .
curl -s localhost:8082/healthz | jq .
curl -s localhost:8081/healthz | jq .
open http://localhost:4171    # NSQ admin
open http://localhost:9090    # Prometheus
open http://localhost:3000    # Grafana (admin/admin)
```
