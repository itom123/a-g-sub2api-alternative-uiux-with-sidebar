UPDATE openclaw_connection_catalog
SET description = CASE kind
    WHEN 'wechat' THEN 'Scan a personal WeChat QR code through the UK02 bridge.'
    WHEN 'telegram' THEN 'Connect a Telegram bot with its BotFather token.'
    WHEN 'gws' THEN 'Authorize Google Workspace through a private OAuth flow.'
    ELSE description
END
WHERE kind IN ('wechat', 'telegram', 'gws');
