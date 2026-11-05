//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpsJobHeartbeatPolicyDefaultsFollowJobCadence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 20, 3, 45, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { return timePtr(now.Add(-d)) }
	tests := []struct {
		name   string
		hb     *OpsJobHeartbeat
		failed bool
	}{
		{"daily cleanup 18h after its run", &OpsJobHeartbeat{JobName: opsCleanupJobName, LastSuccessAt: ago(18 * time.Hour)}, false},
		{"daily cleanup that missed a day", &OpsJobHeartbeat{JobName: opsCleanupJobName, LastSuccessAt: ago(27 * time.Hour)}, true},
		{"daily pre-aggregation one hour after its run", &OpsJobHeartbeat{JobName: opsAggDailyJobName, LastSuccessAt: ago(58 * time.Minute)}, false},
		{"daily pre-aggregation that missed three runs", &OpsJobHeartbeat{JobName: opsAggDailyJobName, LastSuccessAt: ago(3*time.Hour + time.Minute)}, true},
		{"hourly pre-aggregation that missed one tick", &OpsJobHeartbeat{JobName: opsAggHourlyJobName, LastSuccessAt: ago(24 * time.Minute)}, false},
		{"hourly pre-aggregation stalled", &OpsJobHeartbeat{JobName: opsAggHourlyJobName, LastSuccessAt: ago(31 * time.Minute)}, true},
		{"minute job keeps the 15-minute limit", &OpsJobHeartbeat{JobName: opsMetricsCollectorJobName, LastSuccessAt: ago(16 * time.Minute)}, true},
		{"unknown job keeps the 15-minute limit", &OpsJobHeartbeat{JobName: "job-a", LastSuccessAt: ago(14 * time.Minute)}, false},
		{"error after the last success", &OpsJobHeartbeat{JobName: opsCleanupJobName, LastSuccessAt: ago(time.Hour), LastErrorAt: ago(time.Minute)}, true},
		{"never ran", &OpsJobHeartbeat{JobName: opsCleanupJobName}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var policies opsJobHeartbeatPolicies
			require.Equal(t, tt.failed, policies.policyFor(tt.hb.JobName).failed(now, tt.hb))
		})
	}
}

func TestOpsCronNextRunFuncUsesTheLaterSchedule(t *testing.T) {
	t.Parallel()

	lastRun := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC) // a Sunday
	daily := opsJobHeartbeatPolicy{nextRun: opsCronNextRunFunc(time.UTC, "0 2 * * *")}
	hb := &OpsJobHeartbeat{JobName: opsCleanupJobName, LastSuccessAt: &lastRun}
	require.False(t, daily.failed(time.Date(2026, 11, 2, 2, 59, 0, 0, time.UTC), hb), "within the grace hour")
	require.True(t, daily.failed(time.Date(2026, 11, 2, 3, 1, 0, 0, time.UTC), hb), "missed the next daily run")

	weekly := opsJobHeartbeatPolicy{nextRun: opsCronNextRunFunc(time.UTC, "0 2 * * *", "0 2 * * 0")}
	require.False(t, weekly.failed(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), hb), "the later weekly schedule decides")
	require.True(t, weekly.failed(time.Date(2026, 11, 8, 3, 1, 0, 0, time.UTC), hb))

	require.Nil(t, opsCronNextRunFunc(time.UTC, "", "not a cron"))
}

func TestOpsServiceJobHeartbeatPoliciesFollowConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Timezone: "UTC"}
	cfg.Ops.Aggregation.Enabled = false
	cfg.Ops.Cleanup.Enabled = false
	policies := (&OpsService{cfg: cfg}).opsJobHeartbeatPolicies()
	require.True(t, policies.policyFor(opsAggHourlyJobName).skip)
	require.True(t, policies.policyFor(opsAggDailyJobName).skip)
	require.True(t, policies.policyFor(opsCleanupJobName).skip)
	require.False(t, policies.policyFor(opsMetricsCollectorJobName).skip)

	cfg = &config.Config{Timezone: "UTC"}
	cfg.Ops.Aggregation.Enabled = true
	cfg.Ops.Cleanup.Enabled = true
	cfg.Ops.Cleanup.Schedule = "0 3 * * *"
	policies = (&OpsService{cfg: cfg}).opsJobHeartbeatPolicies()
	require.False(t, policies.policyFor(opsAggHourlyJobName).skip)
	cleanup := policies.policyFor(opsCleanupJobName)
	require.False(t, cleanup.skip)
	require.NotNil(t, cleanup.nextRun)
	// The settings default (02:00) and the config schedule (03:00) disagree;
	// the later one is used so neither can raise a false alarm.
	next := cleanup.nextRun(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC))
	require.Equal(t, time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC), next.UTC())
}

// The production overview at 2026-11-01 20:03 UTC: a clean hour (353 requests,
// no errors) that the old formula scored 62 because TTFT P99 was 12s and the
// daily jobs looked stale.
func TestComputeDashboardHealthScoreCleanHourWithLongPrompts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 20, 3, 45, 0, time.UTC)
	at := func(v string) *time.Time {
		parsed, err := time.Parse(time.RFC3339, v)
		require.NoError(t, err)
		return &parsed
	}
	ov := &OpsDashboardOverview{
		SuccessCount:      353,
		RequestCountTotal: 353,
		RequestCountSLA:   353,
		SLA:               1,
		TTFT:              OpsPercentiles{P50: intPtr(3088), P90: intPtr(6678), P95: intPtr(7542), P99: intPtr(11977)},
		SystemMetrics: &OpsSystemMetricsSnapshot{
			DBOK:               boolPtr(true),
			RedisOK:            boolPtr(true),
			CPUUsagePercent:    float64Ptr(23.5),
			MemoryUsagePercent: float64Ptr(83.6),
		},
		JobHeartbeats: []*OpsJobHeartbeat{
			{JobName: opsAlertEvaluatorJobName, LastSuccessAt: at("2026-11-01T20:02:56Z")},
			{JobName: opsCleanupJobName, LastSuccessAt: at("2026-11-01T02:00:00Z")},
			{JobName: opsMetricsCollectorJobName, LastSuccessAt: at("2026-11-01T20:02:53Z")},
			{JobName: opsAggDailyJobName, LastSuccessAt: at("2026-11-01T19:05:49Z")},
			{JobName: opsAggHourlyJobName, LastSuccessAt: at("2026-11-01T19:55:49Z")},
			{JobName: opsScheduledReportJobName, LastSuccessAt: at("2026-11-01T20:02:49Z")},
		},
	}
	require.Equal(t, 100.0, computeInfraHealth(now, ov, nil))
	require.Equal(t, 96, computeDashboardHealthScore(now, ov, nil))
}
