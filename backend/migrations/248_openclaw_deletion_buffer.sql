ALTER TABLE openclaw_instances
    ADD COLUMN IF NOT EXISTS deletion_requested_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS deletion_scheduled_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_openclaw_instances_deletion_queue
    ON openclaw_instances (deletion_scheduled_at)
    WHERE deletion_scheduled_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS openclaw_user_cooldowns (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    deleted_at TIMESTAMPTZ NOT NULL,
    recreate_allowed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
