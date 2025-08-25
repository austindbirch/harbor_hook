# PRD

## Background
Modern SaaS product need to notify third-party systesm when events occur. Teams re-build webhook delivery repeatedly, yet delivering reliably across untrusted networks is hard--retries, idempotency, signing, rate limiting, observability, and runbooks. HarborHook provides a single, reliable service that devs can integrate once to publish events and let the platform handle delivery guarantees and ergonomics.

## Problem Statement
Sending webhooks that are reliable, observable, and secure is non-trvial. Product teams need:
- At-least-once delivery with retries/backoff and DLQ
- Strong observability to debug failed deliveries
- Simple APIs, clear auth, and secure payloads

## Goals and Non-Goals
Goals
1. Multi-tenant webhook delivery with at-least-one semantics
2. APIs to manage endpoints and subscriptions, publish events, query status, and replace DLQ
3. Baked-in Reliability: exponential backoff w/ jitter, idempotency on publish, per-endpoint rate limiting, circuit breaker patterns, DLQ
4. Observability: Prom metrics, Grafana dashboards, OTel tracing (to Tempo??), structured logs (to Loki??)
5. Security: JWT auth at API gateway, mTLS in cluster, HMAC signatures, max payload limits.
6. Portable deployment: docker-compose for local, Helm on kind/minikube for k8s

Non-Goals
1. UI console beyond minimal admin pages or CLI
2. Multi-region active/active and global LB
3. Complex billing and tenant invoicing (expose usage metrics only)
4. Third-party provider plugins beyond core HTTP POST

## Personas and Use Cases
Personas
- Internal dev team: Publishes domain events and delegates delivery to HarborHook
- Partner integrator: Hosts an endpoint, verifies signatures, and wants reliable delivery/replay
- SRE/on-call: Monitors SLOs, triages incidents, replays DLQ, tunes rate limits

Top use cases
- Create a tenant endpoint and subscribe it to a type of webhook event
- Publish events with idempotency keys; platform signs and delivers with retries
- Inspect a failed delivery in Grafana/Tempo, replay from DLQ after fix
- Throttle misbehaving endpoints and prevent noisy neighbors

## User Stories and Acceptance Criteria
- As a tenant admin, I can CRUD webhook endpoints with a shared secret and headers
    - AC: POST returns endpoint id; secret is write-only; GET never reveals plaintext secret
- As a tenant admin, I can subscribe endpoints to event types
    - AC: Publishing `event_type=X` results in queued deliveries to all subscribed endpoints
- As a service, I can publish an event via gRPC/REST with `idempotency_key`
    - AC: Replays with same (`tenant_id`, `idempotency_key`) within 24h are 200 OK and do not create duplicate events
- As a platform, I retry failed deliveries with exponential backoff and jitter
    - AC: Configurable retry schedule (e.g. 1s, 4s, 16s, 64s, etc) up to `max_attempts` then DLQ
- As an SRE/tenant admin, I can list DLQ items and replay
    - AC: Replay produces a new delivery trace and updates status
- As a receiver, I can verify the authenticity of a delivery I receive
    - AC: Deliveries include X-HarborHook-Signature with timestamp + HMAC-SHA256 over the exact body; requests older than N minutes can be rejected by receiver
- As an SRE, I can see p50/p90/p99 delivery latency, success rate, retry rates, backlog depth, per-tenant usage
    - AC: Grafana dashboards sourced from Prom/Loki/Tempo; traces show publish-->queue-->worker-->HTTP

## Scope
MVP
- Tenancy (simple `tenant_id` passed via JWT claims)
- Endpoints, subscriptions CRUD (gRPC + REST)
- Publish events (gRPC + REST via grpc-gateway)
- Dispatcher + Worker with NSQ
- Retries/backoff/jitter, per-endpoint token bucket, circuit breaker pattern
- HMAC sigs + request timestamp
- DLQ + replay API
- OSS observability stack (Prom, Grafaka, Loki, Tempo)
- Kubernetes deployment (kind/minikube) + Helm charts
- CI via github actions: build, test, containerize, kind e2d

Stretch goals
- Per-tenant analytics and quotas via API

## Requirements
Functional
1. Tenancy: All APIs scoped by `tenant_id` (from JWT). No cross-tenant leakage
2. Endpoint management: URL (HTTPS only), headers (allowlist), secret (write-only), rate limit per second, maybe max inflight
3. Subscriptions: Map `event_type` --> one or more endpoints
4. Publish API: Accept `payload` (raw JSON bytes), `event_type`, `idempotency_key`, `occurred_at`
5. Delivery:
    - Sign as `X-HarborHook-Signature: t=<unix>, s256=<hex(hmac_sha256(secret, body||t))>`
    - `Content-Type: application/json`, POST body is exactly `payload`
    - Timeouts: connect 2s, overall 5s (config-able). Max payload 1 mb
    - HTTP 2xx = success; 3xx/4xx/5xx = retry policy (treat 410/404 as terminal? MVP: treat all non-2xx as retryable until `max_attempts`)
6. Retries: Backoff schedule configurable, jitter 20-40%, `max_attempts` default 8
7. DLQ: On `max_attempts` exceeded --> DLQ table/stream, include `reason`, last HTTP status, last error
8. Replay: API to replay single or batch with filter, annotate new delivery with `replay_of = delivery_id`
9. Rate limiting: Token bucket per endpoint (default 10 rps, burst 20). Return 429 internally to reschedule
10. Circuit breaker: Open after N consecutive failure or p95 latency > threshold, half-open after cooldown
11. Status APIs: Get delivery history by `event_id`, endpoint, or time range
12. OpenAPI/Swagger for REST: generated via grpc-gateway annotations

Non-Functional
1. Reliability SLO: ≥99% of deliveries succeed within 30s over 30-day window
2. Latency: p95 publish-->ack enqueue < 50ms (local dev), p95 delivery (first attempt) < 2s under nominal load
3. Throughput (dev target): 500 events/sec sustained on a laptop, scales horizontally by workers
4. Durability: Events persisted before ack to publisher
5. Security: JWT (RS256) at gateway, mTLS between services (cert-manager), secrets in k8s, HTTPS only outbound, header allow-list to prevent header injection
6. Observability: 100% sampling for traces in dev, configurable in prod. Metrics include counters, histograms, gauges listed below
7. Cost: As free as possible. Envoy, Prometheus, Grafana, Loki, Tempo/Jaeger, NSQ, Postgres. Check for student discounts if available, but mostly use open source stuff.

## SLOs, SLIs
Primary SLO
- 99% of deliveries succeed within 30s (including retries) over a 30-day window
- SLIs: from Prometheus histograms + success counters
- Burn alerts: Page if 2-hour burn rate > 4% and 24-hour > 1%. Ticket if 6-hour burn rate > 1%
- Other alerts for worker backlog above threshold for 10m, circuit breaker open for >5m on any endpoint, DLQ spikes (>3 std.dev)

## Runbooks for demoing
- DLQ Spike: Inspect receiver uptime, verify DNS/TLS, test replay on sample, coordinate with tenant, batch replay off-hours
- Backlog Growth: Scale workers (HPA), review rate limits, investigate slow receivers, open circuit temporarily
- High Latency: Check recent deploy, network egress, receiver latency, tune timeout/backoff

## Rollout plan
- Dev: docker-compose one-liner
- CI: kind cluster-->Helm install-->e2e: publish N events, simulate 500/timeout, assert DLQ+replay
- Demo: script + Grafana/Tempo screenshots, fake receiver that returns 500 twice then 200 to show retries