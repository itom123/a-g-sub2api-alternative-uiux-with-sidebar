-- Give every OpenClaw workspace one ordinary customer API key. The link is
-- separate from api_keys so the existing key list, auth cache, quota, balance
-- billing, and usage-log paths remain the source of truth.
CREATE TABLE IF NOT EXISTS openclaw_api_keys (
    instance_id BIGINT PRIMARY KEY REFERENCES openclaw_instances(id) ON DELETE CASCADE,
    api_key_id BIGINT NOT NULL UNIQUE REFERENCES api_keys(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_openclaw_api_keys_api_key_id
    ON openclaw_api_keys (api_key_id);
