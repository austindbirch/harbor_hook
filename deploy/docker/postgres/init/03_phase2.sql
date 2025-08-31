-- Phase 2 DB changes

-- 1. Idempotency: add key + unique constraints (per-tenant)
ALTER TABLE harborhook.events
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

-- Postgres UNIQUE allows multiple NULLs, so we don't need a partial index
ALTER TABLE harborhook.events
  ADD CONSTRAINT uq_events_tenant_idem UNIQUE (tenant_id, idempotency_key);

-- 2. DLQ table
CREATE TABLE IF NOT EXISTS harborhook.dlq (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  delivery_id  UUID NOT NULL REFERENCES harborhook.deliveries(id) ON DELETE CASCADE,
  reason       TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
