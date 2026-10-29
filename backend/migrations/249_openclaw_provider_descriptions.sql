-- Keep the catalog honest until each provider has a real tenant-bound bridge.
UPDATE openclaw_connection_catalog
SET description = CASE kind
    WHEN 'wechat' THEN 'Personal WeChat QR bridge (not enabled yet).'
    WHEN 'telegram' THEN 'Connect a Telegram bot to this workspace.'
    WHEN 'gws' THEN 'Google Workspace OAuth bridge (not enabled yet).'
    ELSE description
END,
updated_at = NOW()
WHERE kind IN ('wechat', 'telegram', 'gws');
