//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type sharingGroupRepo struct {
	GroupRepository
	groups map[int64]*Group
}

func (r *sharingGroupRepo) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	g := r.groups[id]
	if g == nil {
		return nil, ErrGroupNotFound
	}
	return g, nil
}
func TestValidateSubscriptionSharing(t *testing.T) {
	sub := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription}
	standard := &Group{ID: 3, SubscriptionType: SubscriptionTypeStandard}
	repo := &sharingGroupRepo{groups: map[int64]*Group{2: sub, 3: standard}}
	svc := &adminServiceImpl{groupRepo: repo}
	cases := []struct {
		name    string
		groupID int64
		typ     string
		ids     []int64
		wantErr bool
	}{
		{"valid", 9, SubscriptionTypeStandard, []int64{2}, false}, {"subscription target", 9, SubscriptionTypeSubscription, []int64{2}, true}, {"non subscription source", 9, SubscriptionTypeStandard, []int64{3}, true}, {"missing source", 9, SubscriptionTypeStandard, []int64{8}, true}, {"duplicate", 9, SubscriptionTypeStandard, []int64{2, 2}, true}, {"own id", 2, SubscriptionTypeStandard, []int64{2}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateSubscriptionSharing(context.Background(), tc.groupID, tc.typ, tc.ids)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "INVALID_SUBSCRIPTION_SOURCE_GROUPS")
			} else {
				require.NoError(t, err)
			}
		})
	}
	tooMany := make([]int64, 21)
	for i := range tooMany {
		tooMany[i] = int64(i + 100)
	}
	err := svc.validateSubscriptionSharing(context.Background(), 9, SubscriptionTypeStandard, tooMany)
	require.Error(t, err)
	require.Contains(t, err.Error(), "INVALID_SUBSCRIPTION_SOURCE_GROUPS")
}

// 分组类型切换时清掉另一种共享配置；标准分组的「转扣余额」开关恒为 false。
func TestUpdateGroupNormalizesSubscriptionSharing(t *testing.T) {
	ctx := context.Background()

	repo := &groupPlatformRepoStub{group: &Group{ID: 23, Name: "premium", SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceGroupIDs: []int64{17}}}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.UpdateGroup(ctx, 23, &UpdateGroupInput{SubscriptionType: SubscriptionTypeSubscription})
	require.NoError(t, err)
	require.Empty(t, repo.updated.SubscriptionSourceGroupIDs, "subscription groups cannot draw on other subscriptions")

	repo = &groupPlatformRepoStub{group: &Group{ID: 23, Name: "premium", SubscriptionType: SubscriptionTypeStandard}}
	svc = &adminServiceImpl{groupRepo: repo}
	overflow := true
	_, err = svc.UpdateGroup(ctx, 23, &UpdateGroupInput{SubscriptionOverflowToBalance: &overflow})
	require.NoError(t, err)
	require.False(t, repo.updated.SubscriptionOverflowToBalance)

	repo = &groupPlatformRepoStub{group: &Group{ID: 17, Name: "订阅-A", SubscriptionType: SubscriptionTypeSubscription}}
	svc = &adminServiceImpl{groupRepo: repo}
	_, err = svc.UpdateGroup(ctx, 17, &UpdateGroupInput{SubscriptionOverflowToBalance: &overflow})
	require.NoError(t, err)
	require.True(t, repo.updated.SubscriptionOverflowToBalance)
}
