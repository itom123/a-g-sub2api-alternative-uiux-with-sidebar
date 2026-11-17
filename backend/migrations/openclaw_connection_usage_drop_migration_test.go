package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 247 added per-connection counters that nothing on the control plane can
// write; activity comes from the billing key's usage_logs instead.
func TestDropOpenClawConnectionUsageMigration(t *testing.T) {
	content, err := FS.ReadFile("264_drop_openclaw_connection_usage.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "DROP INDEX IF EXISTS idx_openclaw_connections_usage;")
	require.Contains(t, sql,
		"ALTER TABLE openclaw_connections DROP COLUMN IF EXISTS usage_count, DROP COLUMN IF EXISTS last_used_at;")
}
