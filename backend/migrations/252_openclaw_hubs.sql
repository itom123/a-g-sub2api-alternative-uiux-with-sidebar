-- Hub registry for future regional placement and migration. UK02 is the first
-- registered hub; lifecycle routing remains private to the control plane.
CREATE TABLE IF NOT EXISTS openclaw_hubs (
    id BIGSERIAL PRIMARY KEY,
    slug VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(100) NOT NULL,
    region VARCHAR(32) NOT NULL,
    country_policy VARCHAR(64) NOT NULL DEFAULT 'deny-mainland-china-on-uk',
    bridge_url TEXT NOT NULL DEFAULT '',
    max_cells INTEGER NOT NULL DEFAULT 6 CHECK (max_cells > 0),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO openclaw_hubs (slug, display_name, region, country_policy, max_cells)
VALUES ('claw-hub-i', 'Claw Hub I', 'uk', 'deny-mainland-china-on-uk', 6)
ON CONFLICT (slug) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    region = EXCLUDED.region,
    country_policy = EXCLUDED.country_policy,
    max_cells = EXCLUDED.max_cells,
    updated_at = NOW();

ALTER TABLE openclaw_instances
    ADD COLUMN IF NOT EXISTS hub_id BIGINT REFERENCES openclaw_hubs(id) ON DELETE RESTRICT;

UPDATE openclaw_instances
SET hub_id = (SELECT id FROM openclaw_hubs WHERE slug = 'claw-hub-i')
WHERE hub_id IS NULL;

UPDATE openclaw_instances
SET worker_host = 'claw-hub-i'
WHERE worker_host = 'default';

CREATE INDEX IF NOT EXISTS idx_openclaw_instances_hub_id
    ON openclaw_instances (hub_id, status);
