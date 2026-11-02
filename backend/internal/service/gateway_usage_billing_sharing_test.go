package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type sharingBillingCache struct {
	BillingCache
	updates           chan cacheWriteTask
	gotUser, gotGroup int64
	gotAmount         float64
}

func (c *sharingBillingCache) UpdateSubscriptionUsage(_ context.Context, userID, groupID int64, amount float64) error {
	c.gotUser = userID
	c.gotGroup = groupID
	c.gotAmount = amount
	return nil
}
func (c *sharingBillingCache) QueueUpdateSubscriptionUsage(userID, groupID int64, amount float64) {
	c.updates <- cacheWriteTask{kind: cacheWriteUpdateSubscriptionUsage, userID: userID, groupID: groupID, amount: amount}
}

func TestBuildUsageBillingCommandSharingModes(t *testing.T) {
	gid := int64(99)
	sub := &UserSubscription{ID: 42, GroupID: 17}
	base := func() *postUsageBillingParams {
		return &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 2, ActualCost: 3.5}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2, GroupID: &gid}, Account: &Account{ID: 3}}
	}
	p := base()
	p.Subscription = sub
	p.IsSubscriptionBill = true
	cmd := buildUsageBillingCommand("shared", nil, p)
	require.Equal(t, int64(42), *cmd.SubscriptionID)
	require.Equal(t, 3.5, cmd.SubscriptionCost)
	require.Zero(t, cmd.BalanceCost)
	p = base()
	p.IsSubscriptionBill = false
	cmd = buildUsageBillingCommand("overflow", nil, p)
	require.Nil(t, cmd.SubscriptionID)
	require.Zero(t, cmd.SubscriptionCost)
	require.Equal(t, 3.5, cmd.BalanceCost)
}
func TestFinalizePostUsageBillingUsesSubscriptionGroupID(t *testing.T) {
	keyGroup := int64(99)
	p := &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 2}, User: &User{ID: 7}, APIKey: &APIKey{ID: 8, GroupID: &keyGroup}, Account: &Account{ID: 9}, Subscription: &UserSubscription{ID: 4, GroupID: 17}, IsSubscriptionBill: true}
	cache := &sharingBillingCache{}
	deps := &billingDeps{billingCacheService: &BillingCacheService{cache: cache}, deferredService: &DeferredService{}}
	finalizePostUsageBilling(context.Background(), p, deps, &UsageBillingApplyResult{Applied: true})
	require.Equal(t, int64(17), cache.gotGroup)
	require.Equal(t, int64(7), cache.gotUser)
	require.Equal(t, 2.0, cache.gotAmount)
}
