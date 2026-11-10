//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// In invitation-only mode, an existing user's affiliate code (shown to users as
// "my invitation code" and shared as /register?aff=CODE) must admit a new user,
// just like an admin-issued invitation code.

const referralInviterID int64 = 7

type referralAffiliateRepoStub struct {
	AffiliateRepository
	codes map[string]int64 // affiliate code -> owner user ID
	bound map[int64]int64  // invitee user ID -> inviter user ID
}

func (r *referralAffiliateRepoStub) EnsureUserAffiliate(_ context.Context, userID int64) (*AffiliateSummary, error) {
	summary := &AffiliateSummary{UserID: userID}
	if inviterID, ok := r.bound[userID]; ok {
		summary.InviterID = &inviterID
	}
	return summary, nil
}

func (r *referralAffiliateRepoStub) GetAffiliateByCode(_ context.Context, code string) (*AffiliateSummary, error) {
	userID, ok := r.codes[code]
	if !ok {
		return nil, ErrAffiliateProfileNotFound
	}
	return &AffiliateSummary{UserID: userID, AffCode: code}, nil
}

func (r *referralAffiliateRepoStub) BindInviter(_ context.Context, userID, inviterID int64) (bool, error) {
	if _, ok := r.bound[userID]; ok {
		return false, nil
	}
	r.bound[userID] = inviterID
	return true, nil
}

type referralInvitationFixture struct {
	auth      *AuthService
	users     *userRepoStub
	redeem    *redeemCodeRepoStub
	affiliate *referralAffiliateRepoStub
}

func newReferralInvitationFixture(t *testing.T, affiliateEnabled bool, inviterStatus string) *referralInvitationFixture {
	t.Helper()
	enabled := "false"
	if affiliateEnabled {
		enabled = "true"
	}
	users := &userRepoStub{
		nextID: 5,
		usersByID: map[int64]*User{
			referralInviterID: {ID: referralInviterID, Email: "inviter@test.com", Status: inviterStatus},
		},
	}
	auth := newAuthService(users, map[string]string{
		SettingKeyRegistrationEnabled:                 "true",
		SettingKeyInvitationCodeEnabled:               "true",
		SettingKeyAffiliateEnabled:                    enabled,
		SettingKeyAuthSourceDefaultEmailGrantOnSignup: "false",
	}, nil, nil)
	redeem := &redeemCodeRepoStub{codesByCode: map[string]*RedeemCode{
		"ADMIN-INVITE": {ID: 11, Code: "ADMIN-INVITE", Type: RedeemTypeInvitation, Status: StatusUnused},
	}}
	affiliate := &referralAffiliateRepoStub{
		codes: map[string]int64{"FRIEND42": referralInviterID},
		bound: map[int64]int64{},
	}
	auth.redeemRepo = redeem
	auth.affiliateService = NewAffiliateService(affiliate, auth.settingService, nil, nil)
	auth.refreshTokenCache = &refreshTokenCacheStub{}
	return &referralInvitationFixture{auth: auth, users: users, redeem: redeem, affiliate: affiliate}
}

func TestRegister_InvitationMode_AcceptsAffiliateCodeInInvitationField(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusActive)

	_, user, err := f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", " friend42 ", "")
	require.NoError(t, err)
	require.Equal(t, int64(5), user.ID)
	require.Len(t, f.users.created, 1)
	require.Equal(t, referralInviterID, f.affiliate.bound[user.ID], "the code that admitted the user binds its owner as inviter")
	require.Empty(t, f.redeem.useCalls, "a referral code is reusable and must not consume an invitation")
}

func TestRegister_InvitationMode_AcceptsAffiliateCodeFromInviteLink(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusActive)

	_, user, err := f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", "", "FRIEND42")
	require.NoError(t, err)
	require.Equal(t, referralInviterID, f.affiliate.bound[user.ID])
	require.Empty(t, f.redeem.useCalls)
}

func TestRegister_InvitationMode_AdminInvitationStillClaimedOnce(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusActive)

	_, user, err := f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", "ADMIN-INVITE", "FRIEND42")
	require.NoError(t, err)
	require.Len(t, f.redeem.useCalls, 1)
	require.Equal(t, int64(11), f.redeem.useCalls[0].id)
	require.Equal(t, referralInviterID, f.affiliate.bound[user.ID], "the invite link's affiliate code is still bound")
}

func TestRegister_InvitationMode_RejectsUnknownCode(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusActive)

	_, _, err := f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", "NOPE1234", "")
	require.ErrorIs(t, err, ErrInvitationCodeInvalid)
	require.Empty(t, f.users.created)
}

func TestRegister_InvitationMode_RequiresACode(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusActive)

	_, _, err := f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", "", "")
	require.ErrorIs(t, err, ErrInvitationCodeRequired)
	require.Empty(t, f.users.created)
}

func TestRegister_InvitationMode_IgnoresAffiliateCodeWhenProgramOff(t *testing.T) {
	f := newReferralInvitationFixture(t, false, StatusActive)

	_, _, err := f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", "FRIEND42", "")
	require.ErrorIs(t, err, ErrInvitationCodeInvalid)

	_, _, err = f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", "", "FRIEND42")
	require.ErrorIs(t, err, ErrInvitationCodeRequired)
	require.Empty(t, f.users.created)
}

func TestRegister_InvitationMode_RejectsDisabledInvitersCode(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusDisabled)

	_, _, err := f.auth.RegisterWithVerification(context.Background(), "new@test.com", "password", "", "", "FRIEND42", "")
	require.ErrorIs(t, err, ErrInvitationCodeInvalid)
	require.Empty(t, f.users.created)
}

func TestOAuthSignup_InvitationMode_AcceptsAffiliateCode(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusActive)

	_, _, err := f.auth.LoginOrRegisterOAuthWithTokenPair(context.Background(), "oauth@test.com", "oauth_user", "", "", "linuxdo")
	require.ErrorIs(t, err, ErrOAuthInvitationRequired)

	tokenPair, user, err := f.auth.LoginOrRegisterOAuthWithTokenPair(context.Background(), "oauth@test.com", "oauth_user", "", "FRIEND42", "linuxdo")
	require.NoError(t, err)
	require.NotNil(t, tokenPair)
	require.Equal(t, referralInviterID, f.affiliate.bound[user.ID])
	require.Empty(t, f.redeem.useCalls)
}

func TestIsAffiliateInvitationCode(t *testing.T) {
	f := newReferralInvitationFixture(t, true, StatusActive)
	require.True(t, f.auth.IsAffiliateInvitationCode(context.Background(), "friend42"))
	require.False(t, f.auth.IsAffiliateInvitationCode(context.Background(), "ADMIN-INVITE"))
	require.False(t, f.auth.IsAffiliateInvitationCode(context.Background(), ""))

	off := newReferralInvitationFixture(t, false, StatusActive)
	require.False(t, off.auth.IsAffiliateInvitationCode(context.Background(), "FRIEND42"))
}
