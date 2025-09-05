-- Phase 1 schema
CREATE EXTENSION IF NOT EXISTS pgcrypto; -- for gen_random_uuid()

CREATE TABLE IF NOT EXISTS harborhook.endpoints (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL,
    url          TEXT NOT NULL,
    secret       TEXT,      -- Used for signing in phase 2
    headers      JSONB NOT NULL DEFAULT '{}'::jsonb,
    rate_per_sec INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS harborhook.subscriptions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    endpoint_id  UUID NOT NULL REFERENCES harborhook.endpoints(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS harborhook.events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    payload       JSONB NOT NULL,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'delivery_status') THEN
        CREATE TYPE delivery_status AS ENUM ('pending', 'ok', 'failed');
    END IF;
END$$;

CREATE TABLE IF NOT EXISTS harborhook.deliveries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id    UUID NOT NULL REFERENCES harborhook.events(id) ON DELETE CASCADE,
    endpoint_id UUID NOT NULL REFERENCES harborhook.endpoints(id) ON DELETE CASCADE,
    status      delivery_status NOT NULL DEFAULT 'pending',
    attempt     INT NOT NULL DEFAULT 0,
    http_status INT,
    latency_ms  INT,
    last_error  TEXT,
    next_try_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_subs_tenant_event ON harborhook.subscriptions(tenant_id, event_type);
CREATE INDEX IF NOT EXISTS idx_events_tenant_created ON harborhook.events(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deliveries_endpoint_status ON harborhook.deliveries(endpoint_id, status);