package service

import (
	"context"
	"testing"
)

type subscriptionAccessUserRepo struct {
	UserRepository
	user *User
}

func (r subscriptionAccessUserRepo) GetByID(context.Context, int64) (*User, error) {
	return r.user, nil
}

func TestSubscriptionAccessAllowedUsesConfiguredAllowlist(t *testing.T) {
	t.Setenv("SUBSCRIPTION_SIDEBAR_ALLOWLIST", "")
	repo := &paymentConfigSettingRepoStub{values: map[string]string{
		SettingSubscriptionSidebarAllowlist: "42;allowed@example.com",
	}}
	svc := &PaymentService{
		configService: &PaymentConfigService{settingRepo: repo},
		userRepo:      subscriptionAccessUserRepo{user: &User{ID: 42, Email: "allowed@example.com", Username: "allowed"}},
	}
	if !svc.SubscriptionAccessAllowed(context.Background(), 42) {
		t.Fatal("expected configured user to be allowed")
	}

	svc.userRepo = subscriptionAccessUserRepo{user: &User{ID: 7, Email: "other@example.com", Username: "other"}}
	if svc.SubscriptionAccessAllowed(context.Background(), 7) {
		t.Fatal("expected unlisted user to be denied")
	}
}

func TestSubscriptionAccessDefaultsToAdministratorsOnly(t *testing.T) {
	repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
	svc := &PaymentService{configService: &PaymentConfigService{settingRepo: repo}, userRepo: subscriptionAccessUserRepo{user: &User{ID: 42, Role: "user", Email: "member@example.com"}}}
	if svc.SubscriptionAccessAllowed(context.Background(), 42) {
		t.Fatal("empty list must deny regular users")
	}
	svc.userRepo = subscriptionAccessUserRepo{user: &User{ID: 1, Role: "admin"}}
	if !svc.SubscriptionAccessAllowed(context.Background(), 1) {
		t.Fatal("administrator must retain configuration access")
	}
}

func TestSubscriptionAccessCannotBeClaimedByChangingUsername(t *testing.T) {
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingSubscriptionSidebarAllowlist: "allowed@example.com"}}
	svc := &PaymentService{configService: &PaymentConfigService{settingRepo: repo}, userRepo: subscriptionAccessUserRepo{user: &User{ID: 7, Email: "other@example.com", Username: "allowed@example.com"}}}
	if svc.SubscriptionAccessAllowed(context.Background(), 7) {
		t.Fatal("username must not grant subscription access")
	}
}
