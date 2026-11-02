ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS subscription_source_group_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS subscription_overflow_to_balance BOOLEAN NOT NULL DEFAULT FALSE;
