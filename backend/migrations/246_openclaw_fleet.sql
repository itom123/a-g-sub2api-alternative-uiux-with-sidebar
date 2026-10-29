CREATE TABLE IF NOT EXISTS openclaw_instances (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_slug VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'planned',
    worker_host VARCHAR(100) NOT NULL DEFAULT 'default',
    port INTEGER NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_openclaw_instances_user_id
    ON openclaw_instances (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS openclaw_connections (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES openclaw_instances(id) ON DELETE CASCADE,
    kind VARCHAR(32) NOT NULL CHECK (kind IN ('wechat', 'telegram', 'gws')),
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    label VARCHAR(100) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, kind, label)
);

CREATE INDEX IF NOT EXISTS idx_openclaw_connections_instance_id
    ON openclaw_connections (instance_id, created_at ASC);
