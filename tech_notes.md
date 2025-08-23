# Example APIs/Messages
These are just example stubs from ideation

## Services, messages, endpoints
`api.webhook.v1.WebhookService`:
```protobuf
service WebhookService {
    rpc CreateEndpoint(CreateEndpointRequest) returns (CreateEndpointResponse);
    rpc CreateSubscription(CreateSubscriptionRequest) returns (CreateSubscriptionResponse);
    rpc PublishEvent(PublishEventRequest) returns (PublishEventResponse);
    rpc GetDeliveryStatus(GetDeliveryStatusRequest) returns (GetDeliveryStatusResponse);
    rpc ReplayDelivery(ReplayDeliveryRequest) returns (ReplayDeliveryResponse);
}
```

Messages:
```protobuf
message CreateEndpointRequest {
  string tenant_id = 1;
  string url = 2;                 // https://...
  string secret = 3;              // write-once
  map<string,string> headers = 4; // e.g., "X-Env": "prod"
  int32 rate_per_sec = 5;         // default 10
}

message PublishEventRequest {
  string tenant_id = 1;
  string event_type = 2;          // e.g., "appointment.created"
  string idempotency_key = 3;     // unique per tenant for 24h
  bytes payload = 4;              // JSON bytes
  google.protobuf.Timestamp occurred_at = 5;
}

message GetDeliveryStatusRequest {
  string tenant_id = 1;
  string event_id = 2;            // or allow by filters (time range, endpoint_id)
}
```

REST (via grpc-gateway):
- `POST /v1/tenants/{tenant_id}/endpoints`
- `POST /v1/tenants/{tenant_id}/subscriptions`
- `POST /v1/tenants/{tenant_id}/events:publish`
- `GET /v1/tenants/{tenant_id}/events/{event_id}/deliveries`
- `POST /v1/tenants/{tenant_id}/deliveries/{delivery_id}:replay`

Delivery request to receivers
- Method: POST
- Body: raw `payload` (JSON)
- Headers:
    - `Content-Type: application/json`
    - `X-HarborHook-Timestamp: <unix>`
    - `X-Harborhook-Signature: s256=<hex(hmac_sha256(secret, body || timestamp))>`
    - plus tenant-configured allow-listed headers

Receiver verification (doc)
- Reject if `abs(now - timestamp) > 5m`
- Compute HMAC over `body || timestamp` with stored secret, compare constant-time

## Data Model (Postgres)
```
tenants(id PK, name, created_at)
endpoints(id PK, tenant_id FK, url, secret_hash, headers JSONB, rate_per_sec INT, created_at)
subscriptions(id PK, tenant_id FK, event_type TEXT, endpoint_id FK, created_at)
events(id PK, tenant_id FK, event_type, payload JSONB/BYTEA, occurred_at, idempotency_key, created_at)
deliveries(id PK, event_id FK, endpoint_id FK, attempt INT, status TEXT, http_status INT, last_error TEXT, latency_ms INT, replay_of UUID NULL, created_at, updated_at)
dlq(event_id FK, endpoint_id FK, reason TEXT, created_at)
```

Key indexes
- `UNIQUE(tenant_id, idempotency_key)`
- `events(tenant_id, created_at DESC)`
- `deliveries(endpoint_id, status)`
- `subscriptions(tenant_id, event_type)`
- `dlq(created_at DESC)`

## Observability
Metrics (Prometheus)
- Counters:
    - `harborhook_events_published_total{tenant_id}`
    - `harborhook_deliveries_total{status, tenant_id, endpoint_id}`
    - `harborhook_retries_total{endpoint_id, reason}`
    - `harborhook_dlq_total{endpoint_id, reason}`
- Histograms:
    - `harborhook_delivery_latency_seconds{tenant_id}` (e2e per attempt)
    - `harborhook_publish_latency_seconds`
- Gauges:
    - `harborhook_worker_backlog{endpoint_id}`
    - `harborhook_circuit_open{endpoint_id}` (open/closed)

Dashboards (Grafana)
- Delivery success rate over time (stacked by status)
- p50/p95/p99 delivery latency
- Retries by reason and endpoint
- Backlog depth and burn rate
- Error budget burn for SLO (recording rules)

Tracing (Tempo/Jaeger)
- Spans: `PublishEvent`, `Enqueue`, `Dispatch`, `HTTP Deliver`, `SignatureCompute`
- Propogate trace IDs in delivery headers (optional `X-Trace-Id`)

Logging (Loki)
- JSON logs (zap): include `tenant_id`, `event_id`, `delivery_id`, `attempt`, `endpoint_id`, `http_status`, `latency_ms`