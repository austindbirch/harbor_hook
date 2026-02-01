---
title: "Glossary"
weight: 50
---

# HarborHook Glossary

A comprehensive dictionary of terms, concepts, and components used in the HarborHook webhooks platform.

## Key Relationships & Mappings

### Trace → Event → Deliveries → Attempts
The core hierarchy of a HarborHook request flows like so:

```
1 Trace ID = 1 Event = N Deliveries (fanout) = M Attempts (retries)
```

**Example:**
```
Trace: e9aea45b7273718dd34179948be9209f
└── Event: user.created (event_id: abc123)
    ├── Delivery #1: delivery_id_1 → Endpoint A
    │   ├── Attempt 1: Failed (retry in 1s)
    │   ├── Attempt 2: Failed (retry in 3s) 
    │   └── Attempt 3: Succeeded
    ├── Delivery #2: delivery_id_2 → Endpoint B
    │   ├── Attempt 1: Failed (retry in 1s)
    │   └── Attempt 2: Succeeded
    └── Delivery #3: delivery_id_3 → Endpoint C
        └── Attempt 1: Succeeded
```

**Key Points:**
- **One trace** spans the entire request lifecycle
- **One event** triggers deliveries to all subscribed endpoints
- **Each endpoint** gets its own independent delivery (separate delivery_id)
- **Each delivery** can have multiple attempts with the same delivery_id
- **Attempts increment** (1, 2, 3...) but maintain the same delivery_id
- **Retries are independent** - one endpoint failing doesn't affect others

### Fanout Pattern
When an event is published, HarborHook automatically creates one delivery per subscribed endpoint. This 1:N relationship means:
- Publishing `user.signup` with 10 subscribed endpoints creates 10 deliveries
- Each delivery tracks independently (different URLs, retry schedules, success/failure)
- All deliveries share the same trace_id for end-to-end observability

## Core Resources

### Endpoint
A target URL where webhook events are delivered. Endpoints represent the destination systems that will receive webhook payloads. Each endpoint has:
- A unique identifier (`endpoint_id`)
- Target URL (HTTPS only)
- Shared secret for HMAC signature verification
- Custom headers (from allowlist)
- Rate limiting configuration
- Circuit breaker settings

### Event
A domain event that triggers webhook deliveries. Events contain:
- Unique identifier (`event_id`) 
- Event type (used for subscription matching)
- JSON payload (raw bytes)
- Tenant identifier
- Timestamp when the event occurred
- Idempotency key for duplicate prevention

### Subscription
A mapping between event types and endpoints. When an event of a specific type is published, all endpoints subscribed to that event type receive delivery attempts. Subscriptions define:
- Event type pattern to match
- Target endpoint
- Tenant scope

### Delivery
A logical unit representing one endpoint's delivery of one event. Each delivery has:
- Unique delivery ID (stays constant across all attempts)
- Event ID being delivered
- Target endpoint ID
- Current status (queued, in-flight, delivered, failed, dead-lettered)
- Timestamps (enqueued, delivered, failed, dead-lettered)
- Total attempt count
- HTTP status code from last attempt
- Error details if failed

### Attempt
A single physical HTTP request made for a delivery. Key characteristics:
- Multiple attempts can exist for one delivery (same delivery_id)
- Attempt number increments: 1, 2, 3, etc.
- Each attempt has its own HTTP status code, latency, and error details
- Attempts are spaced according to the retry/backoff schedule
- Logged individually with the same delivery_id but different attempt numbers

## Process Components

### Ingest Service
The API service responsible for receiving and persisting events. Functions include:
- Accepting events via gRPC/REST APIs
- Validating payloads and tenant authorization
- Storing events in PostgreSQL
- Publishing delivery tasks to NSQ message queue
- Handling idempotency to prevent duplicate events

### Worker
Background processes that consume delivery tasks and make HTTP requests to endpoints. Workers handle:
- Consuming delivery tasks from NSQ queues
- Making HTTP POST requests to endpoint URLs
- Implementing retry logic with exponential backoff
- Circuit breaker patterns for failing endpoints
- Rate limiting per endpoint
- Signing requests with HMAC-SHA256
- Dead letter queue processing for failed deliveries

### Fake Receiver
A test/demo HTTP server that simulates webhook endpoints for development and testing. Can be configured to return different HTTP status codes to test retry and failure scenarios.

## API Methods & Operations

### Publish
The process of creating and sending an event through the HarborHook system:
1. Client calls PublishEvent API with event payload
2. Ingest service validates and stores event in database
3. System queries subscriptions to find matching endpoints (fanout)
4. Creates delivery records for each subscribed endpoint
5. Publishes delivery tasks to NSQ for worker processing
6. Returns event_id and fanout_count to client

### Replay
The process of retrying a failed delivery by creating a new delivery attempt. Replay operations:
- Create a new delivery task from a dead-lettered delivery
- Maintain reference to original delivery ID
- Allow batch replay of multiple failed deliveries
- Include optional reason/annotation for the replay

### Status
Querying the delivery history and current state of events and deliveries. Status APIs provide:
- Delivery attempts for a specific event
- Timeline of delivery states (queued, in-flight, delivered, failed, dead-lettered)
- Filtering by endpoint, time range, or status
- HTTP status codes and error details from failed attempts

### List DLQ (Dead Letter Queue)
Retrieving entries from the dead letter queue for inspection and potential replay. DLQ operations include:
- Listing failed deliveries that exceeded max retry attempts
- Filtering by endpoint or time range
- Providing details on failure reasons and last error messages

## Queue & Messaging

### NSQ
The message queue system used for reliable task distribution. NSQ handles:
- Delivery task queuing with durability guarantees
- Worker load distribution
- Message acknowledgment and redelivery
- Topic-based routing for different task types

### Task
A JSON message published to NSQ containing all information needed for a delivery attempt:
```json
{
  "delivery_id": "uuid",
  "event_id": "uuid", 
  "tenant_id": "tenant1",
  "endpoint_id": "uuid",
  "endpoint_url": "https://api.example.com/webhook",
  "event_type": "user.created",
  "payload": {...},
  "attempt": 2,
  "published_at": "2023-01-01T00:00:00Z",
  "trace_headers": {...}
}
```
Tasks are created by the ingest service and consumed by workers. The `attempt` field increments with each retry of the same delivery.

### DLQ (Dead Letter Queue)
A special queue containing deliveries that failed after exhausting all retry attempts. DLQ entries include:
- Original delivery task details
- Failure reason and last error message
- HTTP status from final attempt
- Timestamp when dead-lettered
- Attempt count when failed

## Authentication & Security

### JWT (JSON Web Token)
Authentication tokens used for API access. JWTs contain:
- Tenant identifier in claims
- Expiration time
- RS256 signature verification
- Used by Envoy gateway for request authorization

### HMAC Signature
Cryptographic signatures added to webhook deliveries for authenticity verification. Format:
- Header: `X-HarborHook-Signature`
- Value: `t=<unix_timestamp>,s256=<hex_hmac_sha256>`
- Computed over: `<request_body>||<timestamp>`
- Uses endpoint's shared secret as key

### JWKS (JSON Web Key Set)
Public key information used to verify JWT signatures. The JWKS server provides:
- RSA public keys for JWT verification
- Key rotation capabilities
- Standard JWKS endpoint format

## Infrastructure Components

### Envoy Gateway
API gateway providing:
- JWT token validation
- TLS termination
- Rate limiting
- Request routing to backend services
- Load balancing

### PostgreSQL
Primary database storing:
- Events and their payloads
- Endpoint configurations
- Subscription mappings
- Delivery attempt history
- Dead letter queue entries

### Grafana
Observability dashboard providing:
- Delivery success/failure rates
- Latency histograms (p50, p90, p99)
- Error rate monitoring
- Queue depth metrics
- Per-tenant usage analytics

### Prometheus
Metrics collection system tracking:
- HTTP request rates and latency
- Queue depths and processing rates
- Delivery attempt success/failure counters
- Circuit breaker state changes
- Resource utilization metrics

### Loki
Log aggregation system collecting:
- Structured application logs
- Delivery attempt details
- Error messages and stack traces
- Request/response logging

### Tempo
Distributed tracing system providing:
- End-to-end request traces
- Span timing for each component
- Error propagation tracking
- Performance bottleneck identification

## Delivery States & Lifecycle

### Queued
Initial state when a delivery task is created and waiting for worker processing.

### In-Flight
State when a worker has picked up the delivery task and is actively making the HTTP request.

### Delivered
Successful state when the endpoint returned a 2xx HTTP status code.

### Failed
Temporary failure state when the endpoint returned non-2xx status or network error occurred. Will be retried according to backoff schedule.

### Dead-Lettered
Final failure state when a delivery has exceeded the maximum retry attempts and is moved to the DLQ.

## Configuration & Control

### Tenant
Multi-tenancy identifier that scopes all resources and operations. Each tenant has isolated:
- Endpoints and subscriptions  
- Events and deliveries
- Rate limits and quotas
- Authentication tokens

### Idempotency Key
Client-provided string used to prevent duplicate event processing. Events with the same tenant ID and idempotency key within 24 hours are deduplicated.

### Circuit Breaker
Protection mechanism that temporarily stops delivery attempts to failing endpoints. States include:
- Closed: Normal operation
- Open: Blocking requests due to high failure rate
- Half-Open: Testing if endpoint has recovered

### Rate Limiting
Token bucket algorithm controlling delivery frequency per endpoint. Prevents overwhelming downstream systems and implements backpressure.

### Retry Schedule
Exponential backoff configuration defining delay between delivery attempts:
- Base delay schedule (e.g., `1s,3s,10s,30s,60s`)
- Jitter percentage (default: 25%) to prevent thundering herd
- Maximum attempts before dead-lettering (default: 5)

**Example with jitter:**
- Attempt 1 fails → wait 1s ±25% (750ms-1.25s) → Attempt 2
- Attempt 2 fails → wait 3s ±25% (2.25s-3.75s) → Attempt 3
- Attempt 3 fails → wait 10s ±25% (7.5s-12.5s) → Attempt 4

### Backoff
The delay strategy between retry attempts. HarborHook uses:
- **Fixed Schedule**: Predefined delays rather than exponential calculation
- **Per-Attempt Mapping**: Attempt 1 uses delay[0], attempt 2 uses delay[1], etc.
- **Final Delay Capping**: After exhausting the schedule, uses the last configured delay

### Jitter
Random variance added to backoff delays to prevent synchronized retries:
- **Purpose**: Avoid thundering herd when many deliveries fail simultaneously
- **Formula**: `final_delay = base_delay * (1 + random(-jitter%, +jitter%))`
- **Range**: Typically ±25% of base delay
- **Benefit**: Spreads retry timing across time to reduce load spikes

## CLI Tool (harborctl)

### harborctl
Command-line interface for interacting with HarborHook APIs. Provides commands for:
- Managing endpoints and subscriptions
- Publishing test events
- Checking delivery status
- Replaying failed deliveries
- Listing DLQ entries
- Health checking services

## Observability & Monitoring

### Trace
A unique identifier that follows a request through the entire HarborHook system. Each trace represents:
- One published event and all its resulting deliveries
- End-to-end timing from publish to final delivery completion
- Error propagation across components (ingest → queue → worker → endpoint)
- Correlation between logs, metrics, and spans across services

**Format**: 32-character hexadecimal string (e.g., `e9aea45b7273718dd34179948be9209f`)

### Span  
Individual operations within a trace, creating a tree of work units:
- `ingest.PublishEvent` - Processing the initial event
- `db.insert_event` - Database operations
- `nsq.publish_task` - Queue operations
- `worker.delivery` - Processing each delivery
- `http.send_webhook` - HTTP requests to endpoints

Spans include timing, success/error status, and metadata attributes.

### Trace Propagation
The mechanism for passing trace context between components:
- HTTP headers (X-Trace-Id) between services
- NSQ message headers for async operations  
- Database correlation for query attribution
- Enables end-to-end request flow visualization


### SLO (Service Level Objective)
Target performance metrics:
- 99% of deliveries succeed within 30 seconds
- p95 publish-to-enqueue latency < 50ms
- p95 first delivery attempt < 2 seconds

### SLI (Service Level Indicator)  
Measured metrics used to evaluate SLO compliance:
- Delivery success rate over time windows
- Latency percentiles from Prometheus histograms
- Error rates by endpoint and tenant

### Runbook
Operational procedures for common incident scenarios:
- DLQ spike investigation and remediation
- Worker backlog scaling procedures  
- High latency debugging steps
- Circuit breaker recovery processes