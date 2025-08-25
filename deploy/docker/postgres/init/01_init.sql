-- Minimal init for phase 0
CREATE SCHEMA IF NOT EXISTS harborhook;

-- Placeholder tables to verify connectivity
CREATE TABLE IF NOT EXISTS harborhook.tenants (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Fake seed data
INSERT INTO harborhook.tenants (id, name) VALUES ('tn_demo', 'Demo Tenant')
ON CONFLICT DO NOTHING;