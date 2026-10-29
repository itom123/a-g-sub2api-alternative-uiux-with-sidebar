ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS eligible_user_ids JSONB;
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS installment_schedule_cents JSONB;
ALTER TABLE payment_sessions ADD COLUMN IF NOT EXISTS plan_name VARCHAR NOT NULL DEFAULT '';
ALTER TABLE payment_sessions ADD COLUMN IF NOT EXISTS installment_schedule_cents JSONB;
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS provider_closed BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE payment_sessions s SET plan_name=p.name FROM subscription_plans p WHERE s.plan_id=p.id AND s.plan_name='';
