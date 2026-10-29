-- Telegram bot: one Telegram private chat per user.
-- bind_code is the user's permanent code for /bind; chat_id is NULL until linked.
CREATE TABLE IF NOT EXISTS telegram_bindings (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    bind_code VARCHAR(32) NOT NULL UNIQUE,
    chat_id BIGINT UNIQUE,
    telegram_username VARCHAR(64) NOT NULL DEFAULT '',
    language VARCHAR(16) NOT NULL DEFAULT '',
    linked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
