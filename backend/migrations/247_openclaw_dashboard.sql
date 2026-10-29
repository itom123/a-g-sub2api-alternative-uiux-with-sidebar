-- One private OpenClaw workspace per customer, with fleet health data kept on
-- the control plane. Connection kinds live in a catalog so new OpenClaw
-- channels do not require another schema migration.
CREATE UNIQUE INDEX IF NOT EXISTS idx_openclaw_instances_one_per_user
    ON openclaw_instances (user_id);

ALTER TABLE openclaw_instances
    ADD COLUMN IF NOT EXISTS last_latency_ms INTEGER NULL,
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NULL;

ALTER TABLE openclaw_connections
    ADD COLUMN IF NOT EXISTS usage_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NULL;

ALTER TABLE openclaw_connections
    DROP CONSTRAINT IF EXISTS openclaw_connections_kind_check;

CREATE TABLE IF NOT EXISTS openclaw_connection_catalog (
    kind VARCHAR(64) PRIMARY KEY,
    title VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO openclaw_connection_catalog (kind, title, description, sort_order)
VALUES
    ('wechat', 'WeChat', 'Personal QR login through the Weixin channel plugin.', 10),
    ('telegram', 'Telegram', 'Connect a Telegram bot to this workspace.', 20),
    ('gws', 'Google Workspace', 'Authorize Google services for this workspace.', 30)
ON CONFLICT (kind) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_openclaw_connections_usage
    ON openclaw_connections (usage_count DESC, last_used_at DESC NULLS LAST);
