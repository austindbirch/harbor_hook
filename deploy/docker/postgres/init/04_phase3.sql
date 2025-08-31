BEGIN;

-- Link a replayed attempt back to its source
ALTER TABLE harborhook.deliveries
ADD COLUMN replay_of UUID REFERENCES harborhook.deliveries(id) ON DELETE SET NULL;

-- Optional reason for replay
ALTER TABLE harborhook.deliveries ADD COLUMN replay_reason TEXT NULL;

-- Timestamps used to build the status timeline
ALTER TABLE harborhook.deliveries
    ADD COLUMN enqueued_at   TIMESTAMPTZ DEFAULT now(),
    ADD COLUMN dequeued_at   TIMESTAMPTZ,
    ADD COLUMN sent_at       TIMESTAMPTZ,
    ADD COLUMN delivered_at  TIMESTAMPTZ,
    ADD COLUMN failed_at     TIMESTAMPTZ,
    ADD COLUMN dlq_at        TIMESTAMPTZ,
    ADD COLUMN error_reason  TEXT;

-- Fast filters
CREATE INDEX IF NOT EXISTS idx_deliveries_event_id        ON harborhook.deliveries(event_id);
CREATE INDEX IF NOT EXISTS idx_deliveries_endpoint_time   ON harborhook.deliveries(endpoint_id, enqueued_at);
CREATE INDEX IF NOT EXISTS idx_deliveries_replay_of       ON harborhook.deliveries(replay_of);

-- Enforce "exactly one pending replay per source"
CREATE UNIQUE INDEX IF NOT EXISTS uq_single_pending_replay
    ON harborhook.deliveries(replay_of)
    WHERE status IN ('queued', 'inflight');

COMMIT;