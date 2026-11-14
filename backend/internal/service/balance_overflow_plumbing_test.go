//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// New users start with balance overflow allowed and enabled; nothing extra is written.
func TestAdminService_CreateUser_BalanceOverflowDefaultsOn(t *testing.T) {
	repo := &userRepoStub{nextID: 20}
	svc := &adminServiceImpl{userRepo: repo}
	user, err := svc.CreateUser(context.Background(), &CreateUserInput{Email: "a@test.com", Password: "strong-pass"})
	require.NoError(t, err)
	require.True(t, user.BalanceOverflowAdminAllowed)
	require.True(t, user.BalanceOverflowEnabled)
	require.True(t, user.BalanceOverflowAllowed())
	require.Empty(t, repo.updated, "defaults must not need a follow-up update")
}

// An admin creating a user with a switch off gets exactly that persisted by a follow-up update.
func TestAdminService_CreateUser_BalanceOverflowExplicitOff(t *testing.T) {
	repo := &userRepoStub{nextID: 21}
	svc := &adminServiceImpl{userRepo: repo}
	off := false
	user, err := svc.CreateUser(context.Background(), &CreateUserInput{Email: "b@test.com", Password: "strong-pass", BalanceOverflowAdminAllowed: &off})
	require.NoError(t, err)
	require.False(t, user.BalanceOverflowAdminAllowed)
	require.True(t, user.BalanceOverflowEnabled)
	require.False(t, user.BalanceOverflowAllowed())
	require.Len(t, repo.updated, 1)
	require.False(t, repo.updated[0].BalanceOverflowAdminAllowed)
}

func TestAPIKeyAuthSnapshotBalanceOverflowRoundtrip(t *testing.T) {
	groupID := int64(23)
	apiKey := &APIKey{
		ID: 90, UserID: 4, GroupID: &groupID, Key: "sk-overflow-roundtrip", Status: StatusActive,
		User:  &User{ID: 4, Status: StatusActive, BalanceOverflowAdminAllowed: true, BalanceOverflowEnabled: false},
		Group: &Group{ID: groupID, Name: "premium", Platform: PlatformComposite, Status: StatusActive, Hydrated: true, SubscriptionSourceAny: true},
	}
	svc := &APIKeyService{}
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))
	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.True(t, materialized.User.BalanceOverflowAdminAllowed)
	require.False(t, materialized.User.BalanceOverflowEnabled)
	require.True(t, materialized.Group.SubscriptionSourceAny)
	require.Equal(t, 28, cached.Snapshot.Version)
}

// "Any subscription" clears a hand-picked list on standard groups (no validation of the stale list),
// and can never be on for a subscription-type group.
func TestUpdateGroup_SubscriptionSourceAnyNormalization(t *testing.T) {
	ctx := context.Background()
	on := true

	repo := &groupPlatformRepoStub{group: &Group{ID: 23, Name: "premium", SubscriptionType: SubscriptionTypeStandard, SubscriptionSourceGroupIDs: []int64{17, 18}}}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.UpdateGroup(ctx, 23, &UpdateGroupInput{SubscriptionSourceAny: &on})
	require.NoError(t, err)
	require.True(t, repo.updated.SubscriptionSourceAny)
	require.Empty(t, repo.updated.SubscriptionSourceGroupIDs)

	repo = &groupPlatformRepoStub{group: &Group{ID: 17, Name: "订阅-A", SubscriptionType: SubscriptionTypeSubscription}}
	svc = &adminServiceImpl{groupRepo: repo}
	_, err = svc.UpdateGroup(ctx, 17, &UpdateGroupInput{SubscriptionSourceAny: &on})
	require.NoError(t, err)
	require.False(t, repo.updated.SubscriptionSourceAny)
}
