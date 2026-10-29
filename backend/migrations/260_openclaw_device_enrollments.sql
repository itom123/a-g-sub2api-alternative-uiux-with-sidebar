-- Additive, initially unused until the native admission gate is enabled.
-- No bearer credential is persisted in this control-plane table.
CREATE TABLE IF NOT EXISTS openclaw_device_enrollments (
    id VARCHAR(128) PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id),
    instance_id BIGINT NOT NULL REFERENCES openclaw_instances(id),
    tenant_slug VARCHAR(64) NOT NULL,
    client VARCHAR(8) NOT NULL CHECK (client IN ('ios', 'macos')),
    state VARCHAR(24) NOT NULL CHECK (state IN (
        'created', 'awaiting_approval', 'approved', 'connected',
        'expired', 'rejected', 'cancelled'
    )),
    ticket_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(ticket_digest) = 32),
    binding JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at = created_at + INTERVAL '5 minutes'),
    CHECK (state NOT IN ('awaiting_approval', 'approved', 'connected') OR binding IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_openclaw_device_enrollments_owner_created
    ON openclaw_device_enrollments(owner_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_openclaw_device_enrollments_instance
    ON openclaw_device_enrollments(instance_id, created_at DESC);

CREATE TABLE IF NOT EXISTS openclaw_device_enrollment_events (
    id BIGSERIAL PRIMARY KEY,
    enrollment_id VARCHAR(128) NOT NULL REFERENCES openclaw_device_enrollments(id),
    owner_id BIGINT NOT NULL,
    instance_id BIGINT NOT NULL,
    state VARCHAR(24) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_openclaw_device_enrollment_events_enrollment
    ON openclaw_device_enrollment_events(enrollment_id, id);
