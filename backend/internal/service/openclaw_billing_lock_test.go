//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenClawBillingAdminChangesLocked(t *testing.T) {
	exclusive := true
	for name, input := range map[string]*UpdateGroupInput{
		"rename":       {Name: "Another group"},
		"rate":         {RateMultiplier: ptrFloat(0.4)},
		"deactivate":   {Status: "inactive"},
		"platform":     {Platform: PlatformAnthropic},
		"subscription": {SubscriptionType: SubscriptionTypeSubscription},
		"exclusive":    {IsExclusive: &exclusive},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{getByID: &Group{ID: 12, Name: OpenClawBillingGroupName, RateMultiplier: 0.3}}
			svc := &adminServiceImpl{groupRepo: repo}
			_, err := svc.UpdateGroup(context.Background(), 12, input)
			require.ErrorIs(t, err, ErrOpenClawBillingLocked)
			require.Nil(t, repo.updated)
		})
	}
}

func TestOpenClawBillingIdentityCannotBeReusedOrDeleted(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{OpenClawBillingGroupName, " OPENAI-龙虾 ", "CLAW"} {
		repo := &groupRepoStubForAdmin{getByID: &Group{ID: 9, Name: "General"}}
		svc := &adminServiceImpl{groupRepo: repo}
		_, err := svc.CreateGroup(ctx, &CreateGroupInput{Name: name, RateMultiplier: 1})
		require.ErrorIs(t, err, ErrOpenClawBillingLocked)
		_, err = svc.UpdateGroup(ctx, 9, &UpdateGroupInput{Name: name})
		require.ErrorIs(t, err, ErrOpenClawBillingLocked)
		require.Nil(t, repo.created)
		require.Nil(t, repo.updated)
	}

	repo := &groupRepoStubForAdmin{getByID: &Group{ID: 12, Name: OpenClawBillingGroupName}}
	svc := &adminServiceImpl{groupRepo: repo}
	require.ErrorIs(t, svc.DeleteGroup(ctx, 12), ErrOpenClawBillingLocked)
	require.ErrorIs(t, svc.DeleteGroupIfEmpty(ctx, 12), ErrOpenClawBillingLocked)
	_, err := svc.DuplicateGroup(ctx, 12, "admin:1", "")
	require.ErrorIs(t, err, ErrOpenClawBillingLocked)
}

func TestOpenClawBillingUserOverridesLocked(t *testing.T) {
	repo := &groupRepoStubForAdmin{getByID: &Group{ID: 12, Name: OpenClawBillingGroupName}}
	rates := &userGroupRateRepoStubForGroupRate{}
	svc := &adminServiceImpl{groupRepo: repo, userGroupRateRepo: rates}
	ctx := context.Background()
	err := svc.BatchSetGroupRateMultipliers(ctx, 12, []GroupRateMultiplierInput{{UserID: 1, RateMultiplier: 0.1}})
	require.ErrorIs(t, err, ErrOpenClawBillingLocked)
	require.Empty(t, rates.syncedEntries)
	_, err = svc.UpdateUser(ctx, 1, &UpdateUserInput{GroupRates: map[int64]*float64{12: ptrFloat(0.1)}})
	require.ErrorIs(t, err, ErrOpenClawBillingLocked)
	// Clearing overrides is allowed; RPM settings remain independent.
	require.NoError(t, svc.BatchSetGroupRateMultipliers(ctx, 12, nil))
}
