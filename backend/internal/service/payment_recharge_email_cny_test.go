package service

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type rechargeEmailUserRepo struct {
	UserRepository
	user *User
	err  error
}

func (r *rechargeEmailUserRepo) GetByID(context.Context, int64) (*User, error) {
	return r.user, r.err
}

// newRechargeEmailCNYFixture stores rate unless it is empty, which leaves the
// setting missing.
func newRechargeEmailCNYFixture(rate string, users UserRepository) (*PaymentService, *alertEmailCapturingGateway) {
	repo := newNotificationEmailMemorySettingRepo()
	if rate != "" {
		repo.values[SettingSubscriptionUSDToCNYRate] = rate
	}
	gateway := &alertEmailCapturingGateway{}
	email := NewEmailService(repo, nil)
	email.SetTransactionalEmailGateway(gateway)
	return &PaymentService{
		configService:            &PaymentConfigService{settingRepo: repo},
		userRepo:                 users,
		notificationEmailService: NewNotificationEmailService(repo, email),
	}, gateway
}

func rechargeEmailOrder() *dbent.PaymentOrder {
	return &dbent.PaymentOrder{ID: 42, UserID: 7, UserEmail: "user@example.com", UserName: "Alex", Amount: 50, PayAmount: 365}
}

func TestRechargeSuccessEmailConvertsLedgerAmountsToCNY(t *testing.T) {
	svc, gateway := newRechargeEmailCNYFixture("7.2", &rechargeEmailUserRepo{user: &User{Balance: 62.5}})

	require.NoError(t, svc.sendBalanceRechargeSuccessNotification(context.Background(), rechargeEmailOrder()))

	data := gateway.onlyEvent(t).Data
	require.Equal(t, "¥360.00", data["recharge_amount"], "the credited USD amount is converted, not the amount paid")
	require.Equal(t, "¥450.00", data["current_balance"])
	require.Equal(t, "42", data["order_id"])
}

func TestRechargeSuccessEmailShowsDashWithoutUsableRate(t *testing.T) {
	for _, rate := range []string{"", "0", "-1", "20", "abc"} {
		svc, gateway := newRechargeEmailCNYFixture(rate, &rechargeEmailUserRepo{user: &User{Balance: 62.5}})

		require.NoError(t, svc.sendBalanceRechargeSuccessNotification(context.Background(), rechargeEmailOrder()), "rate %q", rate)

		data := gateway.onlyEvent(t).Data
		require.Equal(t, "-", data["recharge_amount"], "rate %q", rate)
		require.Equal(t, "-", data["current_balance"], "rate %q", rate)
	}
}

func TestRechargeSuccessEmailShowsDashWhenBalanceCannotBeLoaded(t *testing.T) {
	svc, gateway := newRechargeEmailCNYFixture("7.2", &rechargeEmailUserRepo{err: errors.New("database unavailable")})

	require.NoError(t, svc.sendBalanceRechargeSuccessNotification(context.Background(), rechargeEmailOrder()))

	data := gateway.onlyEvent(t).Data
	require.Equal(t, "¥360.00", data["recharge_amount"])
	require.Equal(t, "-", data["current_balance"])
}

func TestRechargeSuccessEmailOfficialTemplatesRenderCNY(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	for _, locale := range []string{notificationEmailDefaultLocale, notificationEmailLocaleChinese} {
		preview, err := svc.PreviewTemplate(context.Background(), NotificationEmailPreviewInput{
			Event:  NotificationEmailEventBalanceRechargeSuccess,
			Locale: locale,
		})
		require.NoError(t, err)
		require.NotContains(t, preview.HTML, "$", locale)
		require.Contains(t, preview.HTML, "¥350.00", locale)
		require.Contains(t, preview.HTML, "¥435.00", locale)

		official := notificationEmailOfficialTemplates[NotificationEmailEventBalanceRechargeSuccess][locale]
		rendered, err := renderNotificationEmail(
			NotificationEmailEventBalanceRechargeSuccess,
			official.Subject,
			official.HTML,
			map[string]string{"site_name": "Site", "recipient_name": "Alex", "recharge_amount": "-", "current_balance": "-", "order_id": "42"},
			nil,
		)
		require.NoError(t, err)
		require.NotContains(t, rendered.HTML, "$", locale)
		require.Contains(t, rendered.HTML, "<strong>-</strong>", locale)
	}
}

func TestSavedCustomRechargeTemplateDropsDollarPrefix(t *testing.T) {
	rendered, err := renderNotificationEmail(
		NotificationEmailEventBalanceRechargeSuccess,
		"Recharged ${{recharge_amount}}",
		`<p>${{recharge_amount}} / US$ {{ current_balance }}</p><p>Keep $5 bonus</p>`,
		map[string]string{"recharge_amount": "¥360.00", "current_balance": "¥450.00"},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, "Recharged ¥360.00", rendered.Subject)
	require.Equal(t, `<p>¥360.00 / ¥450.00</p><p>Keep $5 bonus</p>`, rendered.HTML)
}
