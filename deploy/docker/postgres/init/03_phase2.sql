-- Phase 2 DB changes

-- 1. Idempotency: add key + unique constraints (per-tenant)
ALTER TABLE harborhook.events
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_events_tenant_idem
    ON harborhook.events(tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- 2. DLQ table
CREATE TABLE IF NOT EXISTS harborhook.dlq (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  delivery_id  UUID NOT NULL REFERENCES harborhook.deliveries(id) ON DELETE CASCADE,
  reason       TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
