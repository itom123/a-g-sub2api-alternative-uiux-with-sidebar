package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type sharingStatusCache struct {
	BillingCache
	status          *SubscriptionCacheData
	userID, groupID int64
}

func (c *sharingStatusCache) GetSubscriptionCache(_ context.Context, userID, groupID int64) (*SubscriptionCacheData, error) {
	c.userID = userID
	c.groupID = groupID
	return c.status, nil
}
func TestCheckBillingEligibilitySharedUsesSourceGroupLimits(t *testing.T) {
	lim := 1.0
	source := &Group{ID: 11, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	key := &Group{ID: 20, SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceGroupIDs: []int64{11}}
	now := time.Now()
	cache := &sharingStatusCache{status: &SubscriptionCacheData{Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour), DailyUsage: 2}}
	svc := &BillingCacheService{cache: cache}
	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 7}, nil, key, &UserSubscription{ID: 3, UserID: 7, GroupID: 11, Group: source}, "")
	require.ErrorIs(t, err, ErrDailyLimitExceeded)
	require.Equal(t, int64(11), cache.groupID)
	require.Equal(t, int64(7), cache.userID)
}
