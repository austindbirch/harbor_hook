# HarborHook
A Go-first, multi-tenant, reliable webhooks platform that showcases platform engineering, systems design, SRE practice, and modern observability. 

## Quickstart
```bash
make proto
make build
make up
# wait ~5-10s
make logs
```

## Access
- Ingest health: http://localhost:8080/healthz
- Worker health: http://localhost:8082/healthz
- Fake receiver: http://localhost:8081/healthz
- NSQ Admin: http://localhost:4171
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000 (admin/admin)

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
