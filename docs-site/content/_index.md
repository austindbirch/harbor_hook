---
title: "Harborhook"
geekdocNav: false
geekdocAlign: center
geekdocAnchor: false
---

# Harborhook

> **A production-grade webhook delivery platform built to demonstrate platform engineering, distributed systems design, and SRE practices.**

[![CI/CD](https://github.com/austindbirch/harbor_hook/actions/workflows/ci.yaml/badge.svg)](https://github.com/austindbirch/harbor_hook/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/austindbirch/harbor_hook)](https://goreportcard.com/report/github.com/austindbirch/harbor_hook)

Harborhook is a multi-tenant, reliable webhook delivery system built with Go. This is a **portfolio/resume project** designed to showcase modern platform engineering practices, operational maturity, and full-stack observability.

{{< button href="https://github.com/austindbirch/harbor_hook" >}}View on GitHub{{< /button >}}

---

## What This Project Demonstrates

{{< columns >}}

### Platform Engineering
- Multi-tenant SaaS architecture with tenant isolation
- Reliable at-least-once delivery with configurable retry policies
- Dead Letter Queue (DLQ) handling and replay capabilities
- Horizontal scaling patterns for stateless services
- gRPC with HTTP/JSON gateway for API flexibility

<--->

### Security & Authentication
- JWT authentication (RS256) with JWKS key rotation
- mTLS for internal service communication
- HMAC-SHA256 webhook signatures for payload verification
- TLS certificate management and rotation procedures

{{< /columns >}}

{{< columns >}}

### Observability & SRE
- Full observability stack: Prometheus, Grafana, Tempo, Alertmanager
- Distributed tracing with OpenTelemetry
- SLO-based alerting with pre-configured alert rules
- Comprehensive operational runbooks for incident response
- Performance metrics: throughput, latency percentiles, error rates

<--->

### DevOps & Infrastructure
- Kubernetes deployment with Helm charts
- GitHub Actions CI/CD pipeline with automated E2E tests
- Docker Compose for local development
- Infrastructure as Code (Helm charts, Kubernetes manifests)
- Multi-platform Docker images (amd64, arm64)

{{< /columns >}}

---

## Architecture Overview

```mermaid
graph LR
    Client[API Client] -->|HTTPS| Envoy[Envoy Gateway<br/>JWT Auth, TLS]
    Envoy -->|mTLS| Ingest[Ingest Service<br/>Go/gRPC]
    Ingest -->|Store Events| DB[(PostgreSQL)]
    Ingest -->|Fanout| NSQ[NSQ Queue]
    NSQ -->|Consume| Worker[Worker Pool<br/>3+ replicas]
    Worker -->|POST + HMAC| Endpoint[Customer<br/>Webhook Endpoint]
    Worker -->|Update Status| DB

    Ingest -->|Metrics/Traces| Obs[Observability Stack<br/>Prometheus, Grafana,<br/>Loki, Tempo]
    Worker -->|Metrics/Traces| Obs

    style Envoy fill:#e1f5ff
    style Ingest fill:#fff4e1
    style Worker fill:#fff4e1
    style NSQ fill:#f0e1ff
    style DB fill:#e1ffe1
    style Obs fill:#ffe1e1
```

**Flow**: Client → Envoy (auth) → Ingest (fanout) → NSQ → Workers → Customer Endpoints

**[Full Architecture Documentation →](/docs/architecture/)**

---

## Documentation

### Getting Started
- [**Quickstart Guide**](/docs/quickstart/) - Get running in <10 minutes
- [**Architecture**](/docs/architecture/) - System design and components

### Operations
- [**Runbooks**](/docs/runbooks/) - Incident response procedures
- [**CLI Guide**](/docs/harborctl/) - Command-line interface usage

### Reference
- [**Signature Verification**](/docs/signature_verification/) - Webhook security
- [**Glossary**](/docs/glossary/) - Terms and definitions