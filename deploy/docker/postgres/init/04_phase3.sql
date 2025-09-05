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

-- Function to automatically update timestamps based on status changes
CREATE OR REPLACE FUNCTION update_delivery_timestamps()
RETURNS TRIGGER AS $$
BEGIN
    -- Set appropriate timestamp when status changes
    CASE NEW.status
        WHEN 'queued' THEN
            -- When status becomes 'queued', set enqueued_at if not already set
            IF OLD.status IS DISTINCT FROM NEW.status AND NEW.enqueued_at IS NULL THEN
                NEW.enqueued_at = now();
            END IF;
            
        WHEN 'inflight' THEN
            -- When status becomes 'inflight', set dequeued_at
            IF OLD.status IS DISTINCT FROM NEW.status THEN
                NEW.dequeued_at = now();
            END IF;
            
        WHEN 'delivered' THEN
            -- When status becomes 'delivered', set delivered_at
            IF OLD.status IS DISTINCT FROM NEW.status THEN
                NEW.delivered_at = now();
            END IF;
            
        WHEN 'failed' THEN
            -- When status becomes 'failed', set failed_at
            IF OLD.status IS DISTINCT FROM NEW.status THEN
                NEW.failed_at = now();
            END IF;
            
        WHEN 'dead' THEN
            -- When status becomes 'dead', set dlq_at
            IF OLD.status IS DISTINCT FROM NEW.status THEN
                NEW.dlq_at = now();
            END IF;
    END CASE;
    
    -- Always update the updated_at timestamp
    NEW.updated_at = now();
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to automatically update timestamps on status changes
DROP TRIGGER IF EXISTS delivery_timestamps_trigger ON harborhook.deliveries;
CREATE TRIGGER delivery_timestamps_trigger
    BEFORE UPDATE ON harborhook.deliveries
    FOR EACH ROW
    EXECUTE FUNCTION update_delivery_timestamps();

-- Also create trigger for INSERT to handle initial enqueued_at
CREATE OR REPLACE FUNCTION set_initial_timestamps()
RETURNS TRIGGER AS $$
BEGIN
    -- Set enqueued_at for new deliveries if not already set
    IF NEW.status = 'queued' AND NEW.enqueued_at IS NULL THEN
        NEW.enqueued_at = now();
    END IF;
    
    -- Set created_at and updated_at
    NEW.created_at = COALESCE(NEW.created_at, now());
    NEW.updated_at = now();
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS delivery_insert_timestamps_trigger ON harborhook.deliveries;
CREATE TRIGGER delivery_insert_timestamps_trigger
    BEFORE INSERT ON harborhook.deliveries
    FOR EACH ROW
    EXECUTE FUNCTION set_initial_timestamps();

COMMIT;