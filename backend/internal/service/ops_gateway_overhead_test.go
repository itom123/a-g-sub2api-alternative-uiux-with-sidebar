//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpsGatewayOverheadSummaryPercentiles(t *testing.T) {
	t.Parallel()

	var r opsGatewayOverheadRecorder
	now := time.Date(2026, 11, 1, 20, 3, 45, 0, time.UTC)
	routing := func(v int64) *int64 { return &v }
	// 100 requests: processing 1..100ms, upload 0..99ms, routing 10ms each;
	// plus one that failed before routing.
	for i := int64(1); i <= 100; i++ {
		r.record(now.Add(-5*time.Minute), i, i-1, routing(10))
	}
	r.record(now.Add(-4*time.Minute), 500, 2000, nil)

	got := r.summary(now, now.Add(-time.Hour), now)
	require.Equal(t, int64(101), got.SampleCount)
	require.Equal(t, 60, got.WindowMinutes)
	// Upload is reported on its own and never added to the overhead percentiles.
	require.Equal(t, 61, *got.P50)  // 51st of [11..110, 500]
	require.Equal(t, 101, *got.P90) // 91st
	require.Equal(t, 110, *got.P99) // 100th
	require.Equal(t, 500, *got.Max)
	require.Equal(t, 91, *got.ProcessingP90)
	require.Equal(t, 10, *got.RoutingP90, "requests without routing are left out of the routing percentile")
	require.Equal(t, 50, *got.UploadP50) // 51st of [0..99, 2000]
	require.Equal(t, 90, *got.UploadP90)
	require.Equal(t, 2000, *got.UploadMax)
}

func TestOpsGatewayOverheadSummaryWindow(t *testing.T) {
	t.Parallel()

	var r opsGatewayOverheadRecorder
	now := time.Date(2026, 11, 1, 20, 3, 45, 0, time.UTC)
	r.record(now.Add(-61*time.Minute), 900, 0, nil) // already out of the window
	r.record(now.Add(-30*time.Minute), 40, 0, nil)
	r.record(now.Add(-2*time.Minute), 20, 0, nil)

	hour := r.summary(now, now.Add(-24*time.Hour), now)
	require.Equal(t, int64(2), hour.SampleCount)
	require.Equal(t, 60, hour.WindowMinutes, "longer dashboard ranges are capped to the hour kept in memory")
	require.Equal(t, 40, *hour.Max)

	recent := r.summary(now, now.Add(-5*time.Minute), now)
	require.Equal(t, int64(1), recent.SampleCount)
	require.Equal(t, 5, recent.WindowMinutes)
	require.Equal(t, 20, *recent.P90)

	// A minute that wraps around the ring replaces the old slot.
	r.record(now.Add(30*time.Minute), 7, 0, nil)
	later := r.summary(now.Add(30*time.Minute), now.Add(30*time.Minute-time.Minute), now.Add(30*time.Minute))
	require.Equal(t, int64(1), later.SampleCount)
	require.Equal(t, 7, *later.P50)

	empty := r.summary(now, now.Add(-time.Hour), now.Add(-2*time.Hour))
	require.Zero(t, empty.SampleCount)
	require.Nil(t, empty.P90)
}

func TestOpsGatewayOverheadBoundsMemoryPerMinute(t *testing.T) {
	t.Parallel()

	var r opsGatewayOverheadRecorder
	now := time.Date(2026, 11, 1, 20, 3, 45, 0, time.UTC)
	for i := 0; i < 3*opsGatewayOverheadSamplesPerMinute; i++ {
		r.record(now, 5, 0, nil)
	}
	got := r.summary(now.Add(time.Second), now.Add(-time.Minute), now.Add(time.Second))
	require.Equal(t, int64(3*opsGatewayOverheadSamplesPerMinute), got.SampleCount)
	require.Len(t, r.buckets[(now.Unix()/60)%opsGatewayOverheadWindowMinutes].samples, opsGatewayOverheadSamplesPerMinute)
	require.Equal(t, 5, *got.P99)
}

func TestOpsGatewayOverheadOnlyInUnfilteredView(t *testing.T) {
	t.Parallel()

	s := &OpsService{}
	now := time.Now()
	s.RecordGatewayOverhead(now, 30, 0, nil)
	groupID := int64(3)
	require.Nil(t, s.gatewayOverheadSummary(&OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now, Platform: "openai"}))
	require.Nil(t, s.gatewayOverheadSummary(&OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now, GroupID: &groupID}))
	got := s.gatewayOverheadSummary(&OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Second)})
	require.NotNil(t, got)
	require.Equal(t, int64(1), got.SampleCount)
}
