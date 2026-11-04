package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func opsLatencyRow(durationP99, ttftP90, ttftP99 float64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"duration_p50", "duration_p90", "duration_p95", "duration_p99", "duration_avg", "duration_max",
		"ttft_p50", "ttft_p90", "ttft_p95", "ttft_p99", "ttft_avg", "ttft_max", "ttft_sample_count",
	}).AddRow(
		float64(1000), float64(2000), float64(3000), durationP99, float64(1500), int64(90000),
		float64(1800), ttftP90, float64(5400), ttftP99, float64(2300), int64(35000), int64(6835),
	)
}

// Merged hourly buckets overstate the tail; the dashboard must report the exact
// window percentiles whenever the raw query answers in time.
func TestOpsExactWindowLatencyReplacesMergedHourlyPercentiles(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	start := time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	approxTTFT := service.OpsPercentiles{P90: intPtrForOpsTest(4200), P99: intPtrForOpsTest(24569)}

	mock.ExpectQuery(`percentile_cont\(0.99\) WITHIN GROUP \(ORDER BY first_token_ms\)`).
		WithArgs(start, end).
		WillReturnRows(opsLatencyRow(231298, 4169, 9650))

	duration, ttft, err := repo.exactWindowLatency(context.Background(), &service.OpsDashboardFilter{StartTime: start, EndTime: end}, start, end, service.OpsPercentiles{}, approxTTFT)
	require.NoError(t, err)
	require.Equal(t, 9650, *ttft.P99)
	require.Equal(t, 4169, *ttft.P90)
	require.Equal(t, 231298, *duration.P99)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsExactWindowLatencyFailurePolicy(t *testing.T) {
	approxDuration := service.OpsPercentiles{P99: intPtrForOpsTest(300000)}
	approxTTFT := service.OpsPercentiles{P99: intPtrForOpsTest(24569)}
	for _, tc := range []struct {
		name      string
		err       error
		wantError bool
	}{
		{"timeout keeps the approximation", context.DeadlineExceeded, false},
		{"cancellation propagates", context.Canceled, true},
		{"database errors propagate", errors.New("database unavailable"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &opsRepository{db: db}
			start := time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)
			mock.ExpectQuery(`percentile_cont\(0.50\) WITHIN GROUP \(ORDER BY duration_ms\)`).WillReturnError(tc.err)

			duration, ttft, err := repo.exactWindowLatency(context.Background(), &service.OpsDashboardFilter{StartTime: start, EndTime: start.Add(24 * time.Hour)}, start, start.Add(24*time.Hour), approxDuration, approxTTFT)
			if tc.wantError {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, approxDuration, duration)
				require.Equal(t, approxTTFT, ttft)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func intPtrForOpsTest(v int) *int { return &v }
