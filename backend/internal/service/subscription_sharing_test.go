package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionBillingGroupSharingCases(t *testing.T) {
	limit := 10.0
	subGroup := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	standard := &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceGroupIDs: []int64{2}}
	sub := &UserSubscription{ID: 9, GroupID: 2, Group: subGroup}
	cases := []struct {
		name string
		key  *Group
		sub  *UserSubscription
		want *Group
	}{
		{"nil subscription", standard, nil, nil},
		{"own subscription", subGroup, &UserSubscription{GroupID: 2}, subGroup},
		{"shared", standard, sub, subGroup},
		{"not listed", &Group{ID: 1}, sub, nil},
		{"nil sub group", standard, &UserSubscription{GroupID: 2}, nil},
		{"standard sub group", standard, &UserSubscription{GroupID: 2, Group: &Group{ID: 2}}, nil},
		{"nil key group", nil, sub, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SubscriptionBillingGroup(tc.key, tc.sub); got != tc.want {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}
}

type sharingSubRepo struct {
	UserSubscriptionRepository
	calls atomic.Int32
	by    map[int64]*UserSubscription
	errs  map[int64]error
}

func (r *sharingSubRepo) GetActiveByUserIDAndGroupID(_ context.Context, _ int64, gid int64) (*UserSubscription, error) {
	r.calls.Add(1)
	if e := r.errs[gid]; e != nil {
		return nil, e
	}
	return r.by[gid], nil
}

func readySharingSub(id, gid int64, group *Group, usage float64) *UserSubscription {
	now := time.Now()
	return &UserSubscription{ID: id, UserID: 7, GroupID: gid, Group: group, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, DailyUsageUSD: usage}
}
func TestResolveBillingSubscriptionSharing(t *testing.T) {
	limit1, limit2 := 1.0, 10.0
	g1 := &Group{ID: 11, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit1}
	g2 := &Group{ID: 12, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit2}
	standard := &Group{ID: 20, SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceGroupIDs: []int64{11, 12}}
	tests := []struct {
		name    string
		key     *Group
		by      map[int64]*UserSubscription
		errs    map[int64]error
		balance bool
		wantID  int64
		wantErr error
	}{
		{"empty list zero calls", &Group{ID: 20}, nil, nil, true, 0, nil},
		{"first passing", standard, map[int64]*UserSubscription{11: readySharingSub(1, 11, g1, 0), 12: readySharingSub(2, 12, g2, 0)}, nil, true, 1, nil},
		{"skip over limit", standard, map[int64]*UserSubscription{11: readySharingSub(1, 11, g1, 2), 12: readySharingSub(2, 12, g2, 0)}, nil, true, 2, nil},
		{"all over limit", standard, map[int64]*UserSubscription{11: readySharingSub(1, 11, g1, 2), 12: readySharingSub(2, 12, g2, 20)}, nil, true, 0, nil},
		{"not found skipped", standard, map[int64]*UserSubscription{12: readySharingSub(2, 12, g2, 0)}, map[int64]error{11: ErrSubscriptionNotFound}, true, 2, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &sharingSubRepo{by: tc.by, errs: tc.errs}
			svc := NewSubscriptionService(nil, repo, nil, nil, nil)
			defer svc.Stop()
			user := &User{ID: 7, BalanceOverflowAdminAllowed: true, BalanceOverflowEnabled: true}
			got, err := svc.ResolveBillingSubscription(context.Background(), user, tc.key, tc.balance)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want=%v", err, tc.wantErr)
			}
			if tc.name == "empty list zero calls" && repo.calls.Load() != 0 {
				t.Fatalf("calls=%d", repo.calls.Load())
			}
			if tc.wantID == 0 && got != nil {
				t.Fatalf("got sub %#v", got)
			}
			if tc.wantID != 0 && (got == nil || got.ID != tc.wantID) {
				t.Fatalf("got=%v want id %d", got, tc.wantID)
			}
		})
	}
}

func TestResolveBillingSubscriptionOverflowAndExpiry(t *testing.T) {
	lim := 1.0
	now := time.Now()
	g := &Group{ID: 30, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim, SubscriptionOverflowToBalance: true}
	base := readySharingSub(3, 30, g, 2)
	for _, tc := range []struct {
		name    string
		bal     bool
		exp     bool
		wantErr error
	}{{"overflow balance", true, false, nil}, {"overflow no balance", false, false, ErrDailyLimitExceeded}, {"overflow disabled", false, false, ErrDailyLimitExceeded}, {"expired", true, true, ErrSubscriptionExpired}} {
		t.Run(tc.name, func(t *testing.T) {
			gg := *g
			if tc.name == "overflow disabled" {
				gg.SubscriptionOverflowToBalance = false
			}
			ss := *base
			if tc.exp {
				ss.ExpiresAt = now.Add(-time.Hour)
			}
			repo := &sharingSubRepo{by: map[int64]*UserSubscription{30: &ss}}
			svc := NewSubscriptionService(nil, repo, nil, nil, nil)
			defer svc.Stop()
			user := &User{ID: 7, BalanceOverflowAdminAllowed: true, BalanceOverflowEnabled: tc.name != "overflow disabled"}
			got, err := svc.ResolveBillingSubscription(context.Background(), user, &gg, tc.bal)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got != nil {
				t.Fatal("overflow should return nil subscription")
			}
		})
	}
}
