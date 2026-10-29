-- Complete the managed CLAW group policy and attach the active OpenAI pool.
UPDATE groups
SET description = 'Managed OpenClaw billing group',
    platform = 'openai',
    is_exclusive = FALSE,
    status = 'active',
    subscription_type = 'standard',
    model_allowlist = '{"models":["gpt-5.6","gpt-5.6-sol","gpt-5.6-terra","gpt-5.6-luna","gpt-6.1-sol","gpt-6-sol","gpt-6-luna"],"enabled":true}'::jsonb,
    max_reasoning_effort = 'max',
    reasoning_effort_mappings = '[{"from":"none","to":"low","match_type":"exact","model":"gpt-6.1-sol"},{"from":"minimal","to":"low","match_type":"exact","model":"gpt-6.1-sol"}]'::jsonb,
    default_mapped_model = 'gpt-5.6-sol',
    max_reasoning_effort_over_limit = 'downgrade',
    updated_at = NOW()
WHERE name = 'CLAW'
  AND deleted_at IS NULL;

INSERT INTO account_groups (account_id, group_id, priority)
SELECT a.id, g.id, a.priority
FROM accounts a
JOIN groups g ON g.name = 'CLAW' AND g.deleted_at IS NULL
WHERE a.platform = 'openai'
  AND a.type = 'oauth'
  AND a.status = 'active'
  AND a.deleted_at IS NULL
ON CONFLICT (account_id, group_id) DO NOTHING;
