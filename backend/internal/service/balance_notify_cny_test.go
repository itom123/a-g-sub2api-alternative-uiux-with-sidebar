package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type alertEmailCapturingGateway struct {
	mu     sync.Mutex
	events []TransactionalEmailEvent
}

func (g *alertEmailCapturingGateway) SendTransactionalEmail(_ context.Context, event TransactionalEmailEvent) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.events = append(g.events, event)
	return nil
}

func (g *alertEmailCapturingGateway) onlyEvent(t *testing.T) TransactionalEmailEvent {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	require.Len(t, g.events, 1)
	return g.events[0]
}

// newAlertEmailCNYFixture stores rate unless it is empty, which leaves the
// setting missing.
func newAlertEmailCNYFixture(rate string) (*BalanceNotifyService, *alertEmailCapturingGateway) {
	repo := newNotificationEmailMemorySettingRepo()
	if rate != "" {
		repo.values[SettingSubscriptionUSDToCNYRate] = rate
	}
	gateway := &alertEmailCapturingGateway{}
	email := NewEmailService(repo, nil)
	email.SetTransactionalEmailGateway(gateway)
	svc := NewBalanceNotifyService(email, repo, nil)
	svc.SetNotificationEmailService(NewNotificationEmailService(repo, email))
	return svc, gateway
}

func TestCNYDisplayMoneyConvertsLedgerUSDToCNY(t *testing.T) {
	require.Equal(t, "¥83.40", cnyDisplayMoney(6.671915).format(12.5))
	require.Equal(t, "-", cnyDisplayMoney(0).format(12.5))
}

func TestLoadCNYDisplayMoneyOnlyTrustsUsableRates(t *testing.T) {
	usable := map[string]float64{"7.2": 7.2, " 6.67 ": 6.67, "19.99": 19.99}
	for stored, want := range usable {
		svc, _ := newAlertEmailCNYFixture(stored)
		require.InDelta(t, want, float64(loadCNYDisplayMoney(context.Background(), svc.settingRepo)), 1e-9, "rate %q", stored)
	}
	for _, stored := range []string{"", "0", "-1", "20", "20.01", "abc", "NaN", "Inf"} {
		svc, _ := newAlertEmailCNYFixture(stored)
		require.Zero(t, loadCNYDisplayMoney(context.Background(), svc.settingRepo), "rate %q", stored)
	}
	require.Zero(t, loadCNYDisplayMoney(context.Background(), nil))
}

func TestBalanceLowAlertSendsCNY(t *testing.T) {
	svc, gateway := newAlertEmailCNYFixture("7")
	svc.sendBalanceLowEmails(context.Background(), []string{"user@example.com"}, 1, "Alice", "user@example.com", 10, 20, "Site", "https://example.com/pay")

	data := gateway.onlyEvent(t).Data
	require.Equal(t, "¥70.00", data["current_balance"])
	require.Equal(t, "¥140.00", data["threshold"])
}

func TestBalanceLowAlertWithoutRateShowsDash(t *testing.T) {
	svc, gateway := newAlertEmailCNYFixture("")
	svc.sendBalanceLowEmails(context.Background(), []string{"user@example.com"}, 1, "Alice", "user@example.com", 10, 20, "Site", "https://example.com/pay")

	data := gateway.onlyEvent(t).Data
	require.Equal(t, "-", data["current_balance"])
	require.Equal(t, "-", data["threshold"])
}

func TestQuotaAlertSendsCNYAndKeepsPercentThreshold(t *testing.T) {
	tests := []struct {
		name          string
		rate          string
		thresholdType string
		threshold     float64
		wantUsed      string
		wantLimit     string
		wantRemaining string
		wantThreshold string
	}{
		{"fixed threshold", "7", thresholdTypeFixed, 25, "¥560.00", "¥700.00", "¥140.00", "¥175.00"},
		{"percentage threshold", "7", thresholdTypePercentage, 30, "¥560.00", "¥700.00", "¥140.00", "30%"},
		{"fixed threshold without rate", "", thresholdTypeFixed, 25, "-", "-", "-", "-"},
		{"percentage threshold without rate", "", thresholdTypePercentage, 30, "-", "-", "-", "30%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, gateway := newAlertEmailCNYFixture(tt.rate)
			dim := quotaDim{name: quotaDimDaily, enabled: true, threshold: tt.threshold, thresholdType: tt.thresholdType, limit: 100}
			svc.sendQuotaAlertEmails(context.Background(), []string{"admin@example.com"}, 42, "acc", "openai", dim, 80, "Site")

			data := gateway.onlyEvent(t).Data
			require.Equal(t, tt.wantUsed, data["quota_used"])
			require.Equal(t, tt.wantLimit, data["quota_limit"])
			require.Equal(t, tt.wantRemaining, data["quota_remaining"])
			require.Equal(t, tt.wantThreshold, data["quota_threshold"])
		})
	}
}

func TestBuiltInAlertBodiesNeverShowDollars(t *testing.T) {
	s := &BalanceNotifyService{}
	for _, money := range []cnyDisplayMoney{7, 0} {
		require.NotContains(t, s.buildBalanceLowEmailBody("u", money, 1, 5, "Site", ""), "$")
		require.NotContains(t, s.buildQuotaAlertEmailBody(1, "n", "p", "d", money, 80, 100, 20, money.format(25), "Site"), "$")
	}

	lowWithoutRate := s.buildBalanceLowEmailBody("u", 0, 1, 5, "Site", "")
	require.Contains(t, lowWithoutRate, `<div class="balance">-</div>`)
	require.Contains(t, lowWithoutRate, "<strong>-</strong>")

	quotaWithoutRate := s.buildQuotaAlertEmailBody(1, "n", "p", "d", 0, 80, 100, 20, "30%", "Site")
	require.Contains(t, quotaWithoutRate, `<span class="metric-value">-</span>`)
	require.Contains(t, quotaWithoutRate, `<span class="metric-value">30%</span>`)
}

func TestAlertEmailTemplatePreviewsNeverShowDollars(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	for _, event := range []string{NotificationEmailEventBalanceLow, NotificationEmailEventAccountQuotaAlert} {
		for _, locale := range []string{notificationEmailDefaultLocale, notificationEmailLocaleChinese} {
			preview, err := svc.PreviewTemplate(context.Background(), NotificationEmailPreviewInput{Event: event, Locale: locale})
			require.NoError(t, err)
			require.NotContains(t, preview.HTML, "$", "%s/%s", event, locale)
			require.Contains(t, preview.HTML, "¥", "%s/%s", event, locale)
		}
	}
}

func TestSavedCustomAlertTemplateDropsDollarPrefix(t *testing.T) {
	rendered, err := renderNotificationEmail(
		NotificationEmailEventBalanceLow,
		"Balance ${{current_balance}}",
		`<p>${{current_balance}} / US$ {{ threshold }}</p><p>Keep $5 bonus</p>`,
		map[string]string{"current_balance": "¥70.00", "threshold": "¥140.00"},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, "Balance ¥70.00", rendered.Subject)
	require.Equal(t, `<p>¥70.00 / ¥140.00</p><p>Keep $5 bonus</p>`, rendered.HTML)

	quota, err := renderNotificationEmail(
		NotificationEmailEventAccountQuotaAlert,
		"Quota",
		`<p>${{quota_used}} / ${{quota_limit}} / ${{quota_remaining}} / ${{quota_threshold}}</p>`,
		map[string]string{"quota_used": "¥560.00", "quota_limit": "¥700.00", "quota_remaining": "¥140.00", "quota_threshold": "30%"},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, `<p>¥560.00 / ¥700.00 / ¥140.00 / 30%</p>`, quota.HTML)
}
