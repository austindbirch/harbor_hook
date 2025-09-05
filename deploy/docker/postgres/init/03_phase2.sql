-- Phase 2 DB changes

-- 1. Migrate delivery_status enum to match protobuf expectations
-- First, let's add the new enum values we need
ALTER TYPE delivery_status ADD VALUE IF NOT EXISTS 'queued';
ALTER TYPE delivery_status ADD VALUE IF NOT EXISTS 'inflight';  
ALTER TYPE delivery_status ADD VALUE IF NOT EXISTS 'delivered';
ALTER TYPE delivery_status ADD VALUE IF NOT EXISTS 'dead';

-- Update existing data to use new enum values
UPDATE harborhook.deliveries SET status = 'queued' WHERE status = 'pending';
UPDATE harborhook.deliveries SET status = 'delivered' WHERE status = 'ok';
-- 'failed' already exists, so no change needed

-- Update the default value for new records
ALTER TABLE harborhook.deliveries ALTER COLUMN status SET DEFAULT 'queued';

-- 2. Idempotency: add key + unique constraints (per-tenant)
ALTER TABLE harborhook.events
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

-- Postgres UNIQUE allows multiple NULLs, so we don't need a partial index
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint 
        WHERE conname = 'uq_events_tenant_idem'
    ) THEN
        ALTER TABLE harborhook.events
        ADD CONSTRAINT uq_events_tenant_idem UNIQUE (tenant_id, idempotency_key);
    END IF;
END$$;

-- 3. DLQ table
CREATE TABLE IF NOT EXISTS harborhook.dlq (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  delivery_id  UUID NOT NULL REFERENCES harborhook.deliveries(id) ON DELETE CASCADE,
  reason       TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
