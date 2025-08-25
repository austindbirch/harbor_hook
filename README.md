# HarborHook
A Go-first, multi-tenant, reliable webhooks platform that showcases platform engineering, systems design, SRE practice, and modern observability. 

## Quickstart
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

## Smoke checks
```bash
curl -s localhost:8080/healthz | jq .
curl -s localhost:8082/healthz | jq .
curl -s localhost:8081/healthz | jq .
open http://localhost:4171    # NSQ admin
open http://localhost:9090    # Prometheus
open http://localhost:3000    # Grafana (admin/admin)
```

## Stack
- Go
- Envoy
- grpc-gateway
- Postgres
- NSQ
- Prometheus
- Grafana
- Loki
- Tempo
- OTel SDK
