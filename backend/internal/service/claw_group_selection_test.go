//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type clawSelectionGroups struct {
	GroupRepository
	groups []Group
}

func (r *clawSelectionGroups) GetByID(_ context.Context, id int64) (*Group, error) {
	for i := range r.groups {
		if r.groups[i].ID == id {
			return &r.groups[i], nil
		}
	}
	return nil, ErrGroupNotFound
}
func (r *clawSelectionGroups) ListActive(context.Context) ([]Group, error) { return r.groups, nil }

func TestClawGroupOrdinaryCreateRejectedAndProvisioningAllowed(t *testing.T) {
	repo, cache := newCreateLimitStubs()
	svc := newCreateLimitService(repo, cache, 0, 0)
	svc.groupRepo = &clawSelectionGroups{groups: []Group{{ID: 42, Name: OpenClawBillingGroupName, Status: StatusActive}}}
	id := int64(42)
	_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "ordinary", GroupID: &id})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	require.Empty(t, repo.created)
	_, err = svc.create(context.Background(), 7, CreateAPIKeyRequest{Name: "workspace", GroupID: &id}, true)
	require.NoError(t, err)
	require.Len(t, repo.created, 1)
}

func TestClawGroupOrdinaryReassignmentRejected(t *testing.T) {
	svc, repo := newUpdateFieldsAPIKeyService(&APIKey{ID: 1, UserID: 7, Key: "sk-test", Status: StatusActive})
	svc.userRepo = &visibilityUserRepo{user: &User{ID: 7}}
	svc.groupRepo = &clawSelectionGroups{groups: []Group{{ID: 42, Name: OpenClawBillingGroupName, Status: StatusActive}}}
	id := int64(42)
	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: &id})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	require.Empty(t, repo.updateFields)
}

func TestClawGroupNotAvailableEvenWhenExplicitlyGranted(t *testing.T) {
	for _, role := range []string{"user", "admin"} {
		t.Run(role, func(t *testing.T) {
			svc := &APIKeyService{
				userRepo:    &visibilityUserRepo{user: &User{ID: 7, Role: role, AllowedGroups: []int64{42}}},
				userSubRepo: &visibilitySubRepo{},
				groupRepo:   &clawSelectionGroups{groups: []Group{{ID: 42, Name: OpenClawBillingGroupName}, {ID: 9, Name: "General"}}},
			}
			groups, err := svc.GetAvailableGroups(context.Background(), 7)
			require.NoError(t, err)
			require.Len(t, groups, 1)
			require.Equal(t, "General", groups[0].Name)
		})
	}
}

func TestClawGroupAdminAssignmentsRejected(t *testing.T) {
	groups := &clawSelectionGroups{groups: []Group{{ID: 42, Name: OpenClawBillingGroupName, Status: StatusActive}}}
	svc := &adminServiceImpl{groupRepo: groups, apiKeyRepo: &apiKeyRepoStubForGroupUpdate{key: &APIKey{ID: 1, UserID: 7}}}
	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(42))
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	svc.apiKeyRepo = &apiKeyRepoStubForGroupUpdate{key: &APIKey{ID: 1, UserID: 7, Group: &groups.groups[0]}}
	_, err = svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(0))
	require.ErrorIs(t, err, ErrOpenClawGroupImmutable)
}

func TestClawGroupAdminBulkReplacementRejected(t *testing.T) {
	svc := &adminServiceImpl{groupRepo: &clawSelectionGroups{groups: []Group{
		{ID: 42, Name: OpenClawBillingGroupName, Status: StatusActive},
		{ID: 9, Name: "General", Status: StatusActive, IsExclusive: true},
	}}}
	_, err := svc.ReplaceUserGroup(context.Background(), 7, 9, 42)
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	_, err = svc.ReplaceUserGroup(context.Background(), 7, 42, 9)
	require.ErrorIs(t, err, ErrOpenClawGroupImmutable)
}
