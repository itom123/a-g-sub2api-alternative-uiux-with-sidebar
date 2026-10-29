-- Retain approval provenance in PostgreSQL before compacting tenant admission
-- history. Never delete device credentials or historical audit records here.
ALTER TABLE openclaw_device_enrollments
    ADD COLUMN IF NOT EXISTS native_archived_receipt JSONB,
    ADD COLUMN IF NOT EXISTS native_retired_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_openclaw_enrollment_retirement_due
    ON openclaw_device_enrollments(instance_id, expires_at)
    WHERE native_retired_at IS NULL;
