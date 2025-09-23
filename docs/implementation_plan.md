# Implementation Plan

---
## Phase 0: Scaffold and Health
- End Goal: A runnable repo with generated protobufs, a bare Ingest service, Postgres connectivity, and NSQ stack up in Docker (plus a fake receiver service)
- What we add:
    - Repo layout
    - Proto definitions + grpc-gateway annotations
    - `docker-compose.yml` with `postgres`, `nsqd`, `nsqlookupd`, `nsqadmin`, `prometheus`, `grafana`, `fake-receiver`
    - Basic Go services: Ingest, with /healthz and DB migrations
    - Make targets (`make proto/build/up/down/test`)
- Demo:
    - make up --> all containers healthy
    - Open nsqadmin and Grafana (empty dashboards for now)
    - curl /healthz and grpc_health_probe
- AC: 
    - [x] Protos compile
    - [x] Gateway runs
    - [x] DB migrates
    - [x] Containers start cleanly
    - [x] Health checks pass

---
## Phase 1: Hello Delivery e2e
- End Goal: First vertical slice. Create endpoint and subscription, publish an event, worker delivers to fake receiver, status recorded as ok
- What we add:
    - Endpoints and subscriptions CRUD (DB + API)
    - PublishEvent (gRPC + REST): validate, write `events` row, fan out to NSQ (`deliveries` topic) once per subscribed endpoint
    - Worker v1: consume `deliveries/workers`, POST JSON to endpoint (5s timeout), mark `deliveries` row `ok` upon 2xx
    - Minimal Prometheus counters: `events_published_total`, `deliveries_total{status}`
- Demo:
    - `POST /v1/tenants/tn_123/endpoints` returns `ep_1`
	- `POST /v1/tenants/tn_123/subscriptions` for `appointment.created` on ep_1
	- `POST /v1/tenants/tn_123/events:publish` with `event_type=appointment.created`
	- Watch nsqadmin --> message flows, fake receiver logs, DB `deliveries.status=ok`
	- Grafana shows counters increment
- AC: 
    - [x] One publish --> one HTTP delivery to the subscribed endpoint
    - [x] DB reflects success
    - [x] Prometheus counters increment

---
## Phase 2: Reliability Core
- End Goal: make delivery safe and secure: no duplicate publishes, signed payloads, backoff on failures, DLQ when exhausted
- What we add:
    - Idempotency on publish via `UNIQUE(tenant_id, idempotency_key)`
    - HMAC signing and timestamp header, receiver verification guide doc
    - Worker retry policy: exponential backoff with jitter, up to `max_attempts`. Implement with `message.Requeue(delay)`
    - DLQ: when attempts exceed, write to `dlq` table (optionally publish to `deliveries_dlq` topic??)
    - Record `attempt`, `http_status`, `latency_ms`, `last_error` in `deliveries`
- Demo:
    - Configure fake receiver to return `500, 500, 200`
    - Publish event, observe retries with increasing delays, final success marked
    - Force `max_attempts = 2` --> message lands in DLQ, visible in DB (and `deliveries_dlq` topic if I add that)
- AC: 
    - [x] Duplicate publish with same (tenant_id, idempotency_key) returns 200 but creates no new event
    - [x] retries obey backoff/jitter
    - [x] DLQ when exceeded
    - [x] Signatures present and receiver can verify

---
## Phase 3: Replay and Status APIs
- End Goal: Operators can inspect and replay failed deliveries, devs can query delivery history.
- What we add:
    - `GetDeliveryStatus(event_id)` and filters (by `endpoint_id`, time range)
	- `ReplayDelivery(delivery_id)` --> republish a new delivery task (annotate `replay_of`)
	- Optional batch replay by filter (time range, endpoint, reason)
	- CLI (`harborctl`) wrappers for status, replay, timeline, and publish
- Demo:
    - Force a DLQ, list DLQ items via REST/CLI
	- Replay a single delivery, confirm new attempt succeeds and is linked to original (via `replay_of`)
	- Query status timeline for `event_id`
- AC: 
    - [x] Replay re-enqueues exactly one new delivery task
    - [x] Status API returns accurate attempt timeline

---
## Phase 4: Gateway and Security
- End Goal: Production-grade edge: Envoy API gateway with JWT auth, TLS termination, body size limits, mTLS for internal gRPC
- What we add:
    - Envoy fronting gRPC and REST (grpc-gateway behind Envoy)
    - JWT verification (RS256), extracting `tenant_id` claim, public key rotation via JWKS
    - Request constraints: HTTPS only, max body 1 mb, gzip, basic request/conn limits
    - mTLS between Envoy<>services (cert‑manager for certs in k8s later, self-signed in Docker dev)
- Demo:
    - Call APIs without JWT --> 401
    - Call with valid JWT (`tenant_id = tn_123`) --> success
    - Confirm TLS termination at Envoy, internal calls use mTLS
- AC: 
    - [x] Unauthed requests = blocked, authed = pass
    - [x] TLS/mTLS enforced, limits applied
    
---
## Phase 5: Observability Stack
- End Goal: Full-stack observability that tells a complete story: metrics, logs, traces, and SLO-based alerts
- What we add:
    - OTel in all service, traces for `PublishEvent` --> `FanOut` --> `NSQ` --> `Worker` --> `HTTP`
    - Prom metrics (counters, histograms):
        - `harborhook_events_published_total{tenant_id}`
	    - `harborhook_deliveries_total{status, tenant_id, endpoint_id}`
	    - `harborhook_delivery_latency_seconds{tenant_id}`
	    - `harborhook_worker_backlog` (local gauge) & (optional) NSQ topic depth via nsqd `/stats` poller
	    - `harborhook_retries_total{reason}` / `dlq_total{reason}`
    - Loki (Promtail) for JSON logs
    - Tempo/Jaeger for traces, propogate `X-Trace-Id` on outbound
    - Grafana dashboards: success rate, pxx latency, retries, DLQ, backlog, error budget burn
    - Alert rules for SLO burn and backlog spikes
- Demo:
    - Publish a mix of successess/failures, open Grafana and see dashboards move
    - Click a trace to follow e2e path, show logs correlated by trace_id
    - Trigger burn alert by making receiver fail for a short window, show alert firing
- AC: 
    - [x] Dashboards load from repo JSON
    - [x] Traces show all spans
    - [x] Alerts evaluate without errors

---
## Phase 6: K8s and CI/CD
- End Goal: One-command cluster bring-up with Helm, automated e2e in CLI
- What we add:
    - Helm charts for Envoy, Ingest, Worker, Postgres, NSQ, Grafana stack
    - K8s manifests: liveness/readiness, resources, PodDisruptionBudgets, HPAs
    - cert-manager for mTLS certs (cluster-issuer)
    - Github actions pipeline: build, test, image push, kind cluster, `helm upgrade --install`, run e2e tests (publish N events, receiver flaky, assert metrics/DB/trace)
    - Image scanning (Trivy) and basic policy checks
- Demo:
    - `make kind-up && make helm-install` (or a script) --> cluster healthy
    - Run e2e, show success, port-forward nsqadmin and Grafana, browse dashboards
    - Kill a worker pod, show deliveries continue (stateless scale-out)
- AC:
    - [ ] Clean install/upgrade
    - [ ] Green e2e
    - [ ] Zero manual post-steps
    - [ ] CI artifacts (k8s test logs, screenshots)

---
## Phase 7: Runbooks, Documentation, Data Seeding
- End Goal: Show that this can be run like a platform: safe deploys, crisp runbooks
- What we add:
    - Runbooks checked in: DLQ spike, backlog growth, high latency, auth/JWT rotation, cert rotation
    - Document polish: diagrams, quickstart, demo scripts, dashboard screenshots, "how to verify signatures"
    - Script that seeds some fake data for testing and demoing--at least a few thousand messages, across multiple tenants/endpoints/subscriptions/delivery states
- Demo:
    - Go through quickstart process in a new env
- AC:
    - [ ] Runbooks are actionable