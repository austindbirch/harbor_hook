I'll eventually diagram this properly on Lucid, but GPT gave me this cool ASCII diagram to use for now


Clients (gRPC/REST)
       |
       v
+-----------------------+
|  Envoy API Gateway    |  (JWT verify, TLS term, rate-limit)
+-----------+-----------+
            |
     gRPC / HTTP (grpc-gateway)
            v
+-----------------------+         +----------------------+
| Ingest API (Go/gRPC)  |  --->   | Postgres (Events DB) |
+-----------+-----------+         +----------+-----------+
            |                               |
     outbox | publish (tx)                  | state
            v                               v
     +------+-------------------------------+----+
     |                NSQ (events)               |
     +---------------+---------------------------+
                     |
              +------+------+
              | Dispatcher  |  (schedules deliveries into per-endpoint queues)
              +------+------+
                     |
                +----+----+     (mTLS)
                | Workers | --- HTTP POST ---> Tenant Endpoints
                +---------+
(OTel → Prometheus/Loki/Tempo; Grafana dashboards)