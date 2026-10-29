-- Shared career concierge profiles and Telegram-delivered schedules.
CREATE TABLE IF NOT EXISTS career_concierge_profiles (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name VARCHAR(120) NOT NULL DEFAULT '',
    roles TEXT NOT NULL DEFAULT '',
    locations TEXT NOT NULL DEFAULT '',
    remote_ok BOOLEAN NOT NULL DEFAULT TRUE,
    salary_min NUMERIC(12, 2) NOT NULL DEFAULT 0,
    notes TEXT NOT NULL DEFAULT '',
    timezone VARCHAR(64) NOT NULL DEFAULT 'Europe/London',
    telegram_chat_id VARCHAR(128) UNIQUE,
    pairing_hash CHAR(64),
    pairing_expires_at TIMESTAMPTZ,
    telegram_paired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS career_concierge_schedules (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cron_expression VARCHAR(100) NOT NULL,
    timezone VARCHAR(64) NOT NULL DEFAULT 'Europe/London',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    openclaw_job_id VARCHAR(128),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_career_concierge_schedules_due
    ON career_concierge_schedules (enabled, timezone);
CREATE INDEX IF NOT EXISTS idx_career_concierge_schedules_user
    ON career_concierge_schedules (user_id, created_at DESC);
