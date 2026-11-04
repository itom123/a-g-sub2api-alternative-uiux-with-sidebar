package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type userInsightsUnsupportedRepo struct{ UsageLogRepository }
type userInsightsCapableRepo struct {
	UsageLogRepository
	query  usagestats.UserInsightsQuery
	result *usagestats.UserKeyActivity
	err    error
	called bool
}

func (r *userInsightsCapableRepo) GetUserKeyActivity(ctx context.Context, q usagestats.UserInsightsQuery) (*usagestats.UserKeyActivity, error) {
	r.query = q
	r.called = true
	return r.result, r.err
}

func TestUsageServiceUserKeyActivityCapabilityAndCancellation(t *testing.T) {
	svc := NewUsageService(&userInsightsUnsupportedRepo{}, nil, nil, nil)
	_, err := svc.GetUserKeyActivity(context.Background(), usagestats.UserInsightsQuery{})
	require.ErrorIs(t, err, ErrUserKeyActivityUnsupported)
	want := &usagestats.UserKeyActivity{Coverage: "retained_logs"}
	repo := &userInsightsCapableRepo{result: want}
	svc = NewUsageService(repo, nil, nil, nil)
	query := usagestats.UserInsightsQuery{Filters: usagestats.UsageLogFilters{UserID: 42, Model: "requested"}, AsOf: time.Now()}
	got, err := svc.GetUserKeyActivity(context.Background(), query)
	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, query, repo.query)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo.called = false
	_, err = svc.GetUserKeyActivity(ctx, query)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, repo.called)
	sentinel := errors.New("aggregation failed")
	repo.err = sentinel
	_, err = svc.GetUserKeyActivity(context.Background(), query)
	require.ErrorIs(t, err, sentinel)
	repo.err = nil
	repo.result = nil
	_, err = svc.GetUserKeyActivity(context.Background(), query)
	require.Error(t, err)
}
