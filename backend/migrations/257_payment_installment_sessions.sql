CREATE TABLE IF NOT EXISTS payment_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    plan_id BIGINT NOT NULL,
    subscription_group_id BIGINT NOT NULL,
    subscription_days INTEGER NOT NULL,
    base_amount DECIMAL(20,2) NOT NULL,
    allocation_order_id BIGINT,
    target_amount DECIMAL(20,2) NOT NULL,
    paid_amount DECIMAL(20,2) NOT NULL DEFAULT 0,
    currency VARCHAR(8) NOT NULL DEFAULT 'CNY',
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    allocation_error TEXT,
    allocated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS payment_sessions_user_status_idx ON payment_sessions(user_id, status);
CREATE INDEX IF NOT EXISTS payment_sessions_user_plan_status_idx ON payment_sessions(user_id, plan_id, status);

ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS payment_session_id BIGINT REFERENCES payment_sessions(id);
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS installment_number INTEGER;
CREATE INDEX IF NOT EXISTS payment_orders_payment_session_idx ON payment_orders(payment_session_id);

ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS fixed_price_cny DECIMAL(20,2) NOT NULL DEFAULT 0;
