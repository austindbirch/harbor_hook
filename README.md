# HarborHook
A Go-first, multi-tenant, reliable webhook delivery platform that showcases platform engineering, systems design, SRE practice, and modern observability practices. 

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

## TODO
- [ ] Should this be implemented as a SaaS product, where we have a UI and API access deployed? Or should this live as a bundled package that a dev could theoretically download and use as a pre-baked webhook service?
- [ ] I'm using docker desktop right now, but other work projects use Rancher. Can I swap docker desktop for rancher?
- [ ] Regardless of whether this is SaaS or a dev package, I need to add a way for users/devs to generate, store, and inject secrets (for event signing)

## Smoke checks
```bash
curl -s localhost:8080/healthz | jq .
curl -s localhost:8082/healthz | jq .
curl -s localhost:8081/healthz | jq .
open http://localhost:4171    # NSQ admin
open http://localhost:9090    # Prometheus
open http://localhost:3000    # Grafana (admin/admin)
```
