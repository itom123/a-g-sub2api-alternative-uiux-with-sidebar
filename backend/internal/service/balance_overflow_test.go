//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type balanceOverflowRepo struct {
	userSubRepoNoop
	active    map[int64]*UserSubscription
	listed    []UserSubscription
	getCalls  int
	listCalls int
}

func (r *balanceOverflowRepo) GetActiveByUserIDAndGroupID(_ context.Context, _, groupID int64) (*UserSubscription, error) {
	r.getCalls++
	s := r.active[groupID]
	if s == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *s
	return &cp, nil
}
func (r *balanceOverflowRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	r.listCalls++
	return r.listed, nil
}

func overflowUser(enabled bool) *User {
	return &User{ID: 7, BalanceOverflowAdminAllowed: true, BalanceOverflowEnabled: enabled}
}
func exhaustedSub(id, gid int64, g *Group, expiry time.Time) *UserSubscription {
	n := time.Now()
	return &UserSubscription{ID: id, UserID: 7, GroupID: gid, Group: g, Status: SubscriptionStatusActive, ExpiresAt: expiry, DailyWindowStart: &n, WeeklyWindowStart: &n, MonthlyWindowStart: &n, DailyUsageUSD: 2}
}

func TestResolveBillingSubscription_UserOverflowMatrix(t *testing.T) {
	lim := 1.0
	g := &Group{ID: 30, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	for _, tc := range []struct {
		name                       string
		admin, user, balance, pass bool
	}{
		{"all enabled", true, true, true, true}, {"admin off", false, true, true, false}, {"user off", true, false, true, false}, {"balance low", true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &balanceOverflowRepo{active: map[int64]*UserSubscription{30: exhaustedSub(1, 30, g, time.Now().Add(time.Hour))}}
			s := NewSubscriptionService(nil, r, nil, nil, nil)
			t.Cleanup(s.Stop)
			u := &User{ID: 7, BalanceOverflowAdminAllowed: tc.admin, BalanceOverflowEnabled: tc.user}
			got, err := s.ResolveBillingSubscription(context.Background(), u, g, tc.balance)
			if tc.pass {
				require.NoError(t, err)
				require.Nil(t, got)
			} else {
				require.ErrorIs(t, err, ErrDailyLimitExceeded)
			}
		})
	}
}

func TestResolveBillingSubscription_SourceAnyOrderingAndOverflow(t *testing.T) {
	lim := 1.0
	g1 := &Group{ID: 11, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	g2 := &Group{ID: 12, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	g3 := &Group{ID: 13, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	standard := &Group{ID: 20, SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceAny: true}
	now := time.Now()
	r := &balanceOverflowRepo{listed: []UserSubscription{*exhaustedSub(3, 13, g3, now.Add(3*time.Hour)), *exhaustedSub(1, 11, g1, now.Add(time.Hour)), *exhaustedSub(2, 12, g2, now.Add(2*time.Hour))}, active: map[int64]*UserSubscription{11: exhaustedSub(1, 11, g1, now.Add(time.Hour)), 12: exhaustedSub(2, 12, g2, now.Add(2*time.Hour)), 13: exhaustedSub(3, 13, g3, now.Add(3*time.Hour))}}
	r.active[12].DailyUsageUSD = 0
	s := NewSubscriptionService(nil, r, nil, nil, nil)
	t.Cleanup(s.Stop)
	got, err := s.ResolveBillingSubscription(context.Background(), overflowUser(false), standard, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.ID)
	r.active[12].DailyUsageUSD = 2
	got, err = s.ResolveBillingSubscription(context.Background(), overflowUser(true), standard, true)
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = s.ResolveBillingSubscription(context.Background(), overflowUser(false), standard, true)
	require.ErrorIs(t, err, ErrDailyLimitExceeded)
	require.Nil(t, got)
}

func TestResolveBillingSubscription_SourceSelectionAvoidsList(t *testing.T) {
	lim := 1.0
	g := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	now := time.Now()
	r := &balanceOverflowRepo{active: map[int64]*UserSubscription{2: {ID: 1, UserID: 7, GroupID: 2, Group: g, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}}}
	s := NewSubscriptionService(nil, r, nil, nil, nil)
	t.Cleanup(s.Stop)
	_, err := s.ResolveBillingSubscription(context.Background(), overflowUser(true), &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceGroupIDs: []int64{2}}, true)
	require.NoError(t, err)
	require.Zero(t, r.listCalls)
	_, err = s.ResolveBillingSubscription(context.Background(), overflowUser(true), &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard}, true)
	require.NoError(t, err)
	require.Zero(t, r.listCalls)
}

func TestSubscriptionBillingGroup_SourceAny(t *testing.T) {
	lim := 1.0
	sg := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	sub := &UserSubscription{GroupID: 2, Group: sg}
	for _, tc := range []struct {
		name string
		any  bool
		list []int64
		want bool
	}{{"any", true, nil, true}, {"listed", false, []int64{2}, true}, {"not listed", false, nil, false}} {
		t.Run(tc.name, func(t *testing.T) {
			g := &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceAny: tc.any, SubscriptionSourceGroupIDs: tc.list}
			require.Equal(t, tc.want, SubscriptionBillingGroup(g, sub) != nil)
		})
	}
}

func TestResolveBillingSubscription_SourceAnyCacheInvalidation(t *testing.T) {
	lim := 1.0
	g := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	standard := &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceAny: true}
	r := &balanceOverflowRepo{listed: []UserSubscription{{ID: 1, UserID: 7, GroupID: 2, Group: g, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}}}
	s := NewSubscriptionService(nil, r, nil, nil, &config.Config{SubscriptionCache: config.SubscriptionCacheConfig{L1Size: 16, L1TTLSeconds: 60}})
	t.Cleanup(s.Stop)
	s.subCacheL1.SetWithTTL(subListCacheKey(7), []subscriptionGroupRef{{GroupID: 2, ExpiresAt: time.Now().Add(time.Hour), ID: 1}}, 1, time.Minute)
	s.subCacheL1.Wait()
	_, _ = s.ResolveBillingSubscription(context.Background(), overflowUser(true), standard, true)
	initial := r.listCalls
	require.GreaterOrEqual(t, initial, 1)
	s.InvalidateSubCacheSync(7, 2)
	_, _ = s.ResolveBillingSubscription(context.Background(), overflowUser(true), standard, true)
	require.Equal(t, initial+1, r.listCalls)
}

var _ = errors.Is
