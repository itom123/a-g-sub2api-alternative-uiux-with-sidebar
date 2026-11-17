-- Migration: 264_drop_openclaw_connection_usage
-- 247 added openclaw_connections.usage_count and last_used_at for a
-- per-connection activity metric. Nothing ever wrote them: WeChat/Telegram
-- traffic flows directly between each OpenClaw cell's channel plugins and the
-- provider, so the control plane never observes a connection's messages.
-- Workspace activity and usage are read from the billing key's usage_logs.
-- The Go service and frontend no longer read or return these columns.

DROP INDEX IF EXISTS idx_openclaw_connections_usage;
ALTER TABLE openclaw_connections
    DROP COLUMN IF EXISTS usage_count,
    DROP COLUMN IF EXISTS last_used_at;
