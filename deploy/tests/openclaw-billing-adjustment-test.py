#!/usr/bin/env python3
"""Exercise the one-time SQL using only temporary tables on a PostgreSQL connection.

Uses normal PG* environment variables. search_path excludes public, so a missing
fixture table fails instead of accessing application data.
"""
import os
from pathlib import Path
import subprocess
import unittest


SCRIPT = (Path(__file__).parents[1] / 'maintenance/openclaw-billing-20261017.sql').read_text()
FIXTURE = """
SET search_path TO pg_temp;
CREATE TEMP TABLE settings (key text PRIMARY KEY, value text, updated_at timestamptz);
CREATE TEMP TABLE groups (id bigint PRIMARY KEY, name text, rate_multiplier numeric(20,4),
    deleted_at timestamptz, platform text DEFAULT 'openai', description text, status text DEFAULT 'active',
    is_exclusive bool, subscription_type text, updated_at timestamptz,
    fallback_group_id bigint, fallback_group_id_on_invalid_request bigint);
CREATE TEMP TABLE users (id bigint PRIMARY KEY, balance numeric(20,8), frozen_balance numeric(20,8),
    deleted_at timestamptz, updated_at timestamptz);
CREATE TEMP TABLE api_keys (id bigint PRIMARY KEY, group_id bigint, deleted_at timestamptz, updated_at timestamptz);
CREATE TEMP TABLE openclaw_api_keys (instance_id bigint, api_key_id bigint);
CREATE TEMP TABLE account_groups (account_id bigint, group_id bigint, priority int, UNIQUE(account_id,group_id));
CREATE TEMP TABLE user_group_rate_multipliers (user_id bigint, group_id bigint, rate_multiplier numeric(20,8),
    rpm_override int, created_at timestamptz, updated_at timestamptz, UNIQUE(user_id,group_id));
CREATE TEMP TABLE user_allowed_groups (user_id bigint, group_id bigint);
CREATE TEMP TABLE user_subscriptions (group_id bigint, deleted_at timestamptz);
CREATE TEMP TABLE subscription_plans (group_id bigint);
CREATE TEMP TABLE channel_groups (group_id bigint);
CREATE TEMP TABLE composite_model_routes (group_id bigint, deleted_at timestamptz);
CREATE TEMP TABLE redeem_codes (code text, type text, value numeric(20,8), status text, used_by bigint,
    used_at timestamptz, notes text, group_id bigint);
CREATE TEMP TABLE channel_monitor_v2_config (group_ids bigint[]);
CREATE TEMP TABLE channel_account_stats_pricing_rules (group_ids bigint[]);
INSERT INTO groups (id,name,rate_multiplier) VALUES
    (6,'openai-管理',0.05),(8,'openai-通用',0.04),(9,'openai-编程',0.05),
    (10,'openai-推理',0.06),(12,'openai-龙虾',0.4),(13,'CLAW',0.4);
INSERT INTO users VALUES (1,1.12345678,0,NULL,NULL),(2,0,3,NULL,NULL),
    (3,-0.12345678,0,NULL,NULL),(4,8,0,now(),NULL);
INSERT INTO api_keys (id,group_id) VALUES (51,13),(56,12);
INSERT INTO openclaw_api_keys VALUES (2,51),(3,56);
INSERT INTO account_groups VALUES (1,12,1),(1,13,2),(2,13,4);
INSERT INTO user_group_rate_multipliers (user_id,group_id,rate_multiplier,rpm_override) VALUES
    (1,8,0.02,10),(1,6,0.03,15),(1,12,0.1,20),(2,13,0.2,30);
"""


class BillingAdjustmentTest(unittest.TestCase):
    def run_sql(self, sql):
        return subprocess.run(
            [os.environ.get('PSQL', 'psql'), '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1'],
            input=FIXTURE + sql, text=True, capture_output=True, check=False,
        )

    def test_exact_decimal_scaling_bindings_history_and_idempotency(self):
        result = self.run_sql(SCRIPT + SCRIPT + """
DO $$ BEGIN
    IF (SELECT balance FROM users WHERE id=1) <> 5.61728390 OR
       (SELECT balance FROM users WHERE id=3) <> -0.61728390 OR
       (SELECT balance FROM users WHERE id=2) <> 0 OR
       (SELECT frozen_balance FROM users WHERE id=2) <> 3 OR
       (SELECT balance FROM users WHERE id=4) <> 8 THEN RAISE EXCEPTION 'balances'; END IF;
    IF (SELECT rate_multiplier FROM groups WHERE id=6) <> 0.05 OR
       (SELECT rate_multiplier FROM groups WHERE id=8) <> 0.20 OR
       (SELECT rate_multiplier FROM groups WHERE id=9) <> 0.25 OR
       (SELECT rate_multiplier FROM groups WHERE id=10) <> 0.30 OR
       (SELECT rate_multiplier FROM groups WHERE id=12) <> 0.30 THEN RAISE EXCEPTION 'rates'; END IF;
    IF (SELECT rate_multiplier FROM user_group_rate_multipliers WHERE group_id=8) <> 0.1 OR
       (SELECT rate_multiplier FROM user_group_rate_multipliers WHERE group_id=6) <> 0.03 OR
       EXISTS (SELECT 1 FROM user_group_rate_multipliers WHERE group_id=12 AND rate_multiplier IS NOT NULL) OR
       (SELECT rpm_override FROM user_group_rate_multipliers WHERE user_id=2 AND group_id=12) <> 30 THEN
       RAISE EXCEPTION 'overrides'; END IF;
    IF EXISTS (SELECT 1 FROM api_keys WHERE group_id<>12) OR
       (SELECT deleted_at FROM groups WHERE id=13) IS NULL OR
       (SELECT count(*) FROM account_groups WHERE group_id=12) <> 2 OR
       (SELECT count(*) FROM redeem_codes WHERE type='admin_balance') <> 3 OR
       (SELECT count(*) FROM settings) <> 1 THEN RAISE EXCEPTION 'references/receipt'; END IF;
END $$;
""")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('already applied', result.stderr)

    def test_unexpected_group_inventory_rejected(self):
        result = self.run_sql("UPDATE groups SET rate_multiplier=0.07 WHERE id=8;" + SCRIPT)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Unexpected live groups/rates', result.stderr)

    def test_code_repair_may_already_have_enforced_canonical_rate(self):
        result = self.run_sql("UPDATE groups SET rate_multiplier=0.3 WHERE id=12;" + SCRIPT)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_ordinary_key_requires_explicit_remediation(self):
        result = self.run_sql("INSERT INTO api_keys(id,group_id) VALUES (99,12);" + SCRIPT)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('ordinary API key', result.stderr)

    def test_unexpected_dependency_rejected(self):
        result = self.run_sql("INSERT INTO channel_groups VALUES(13);" + SCRIPT)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Unexpected operational dependency', result.stderr)


if __name__ == '__main__':
    unittest.main()
