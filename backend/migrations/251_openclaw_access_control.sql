-- Per-user OpenClaw self-service policy and approval requests.
-- A missing policy row means self-service is enabled, so existing users keep
-- the behaviour they had before this migration.
CREATE TABLE IF NOT EXISTS openclaw_user_policy (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    self_service_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS openclaw_access_requests (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action VARCHAR(16) NOT NULL CHECK (action IN ('create', 'delete')),
    instance_id BIGINT NULL REFERENCES openclaw_instances(id) ON DELETE CASCADE,
    reason TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'consumed')),
    reviewed_by BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ NULL,
    review_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_openclaw_access_requests_status
    ON openclaw_access_requests (status, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_openclaw_access_requests_user
    ON openclaw_access_requests (user_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_openclaw_access_requests_pending
    ON openclaw_access_requests (user_id, action, COALESCE(instance_id, 0))
    WHERE status = 'pending';
