//go:build unit

package service

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// Use the actual transaction client for assignment so the policy observes the
// first allocation before a second paid order is allowed to allocate.
type paymentPolicyEntSubRepo struct {
	userSubRepoNoop
	client *dbent.Client
}

func (r *paymentPolicyEntSubRepo) from(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}
func (r *paymentPolicyEntSubRepo) GetByUserIDAndGroupID(ctx context.Context, uid, gid int64) (*UserSubscription, error) {
	o, err := r.from(ctx).UserSubscription.Query().Where(usersubscription.UserIDEQ(uid), usersubscription.GroupIDEQ(gid)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, ErrSubscriptionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &UserSubscription{ID: o.ID, UserID: uid, GroupID: gid, Notes: psStringValue(o.Notes), Status: o.Status, ExpiresAt: o.ExpiresAt}, nil
}
func (r *paymentPolicyEntSubRepo) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	o, err := r.from(ctx).UserSubscription.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &UserSubscription{ID: o.ID, UserID: o.UserID, GroupID: o.GroupID, Notes: psStringValue(o.Notes), Status: o.Status, ExpiresAt: o.ExpiresAt}, nil
}
func (r *paymentPolicyEntSubRepo) Create(ctx context.Context, sub *UserSubscription) error {
	o, err := r.from(ctx).UserSubscription.Create().SetUserID(sub.UserID).SetGroupID(sub.GroupID).SetStartsAt(sub.StartsAt).SetExpiresAt(sub.ExpiresAt).SetStatus(sub.Status).SetNotes(sub.Notes).Save(ctx)
	if err == nil {
		sub.ID = o.ID
	}
	return err
}

func TestFreshPaidOrdersSamePlatformAllocateAtMostOnce(t *testing.T) {
	ctx := context.Background()
	svc, plan, user, _ := newOneTimePolicyTestService(t)
	repo := &paymentPolicyEntSubRepo{client: svc.entClient}
	svc.subscriptionSvc = NewSubscriptionService(svc.groupRepo, repo, nil, nil, nil)
	g2, err := svc.entClient.Group.Create().SetName("another plan group").SetPlatform("anthropic").SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	first := createPaymentFulfillmentSubscriptionOrder(t, ctx, svc.entClient, OrderStatusPaid, time.Now())
	first, err = svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetUserID(user.ID).SetSubscriptionGroupID(plan.GroupID).Save(ctx)
	require.NoError(t, err)
	second := createPaymentFulfillmentSubscriptionOrder(t, ctx, svc.entClient, OrderStatusPaid, time.Now())
	second, err = svc.entClient.PaymentOrder.UpdateOneID(second.ID).SetUserID(user.ID).SetSubscriptionGroupID(g2.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, first.ID))
	require.ErrorIs(t, svc.ExecuteSubscriptionFulfillment(ctx, second.ID), ErrSubscriptionPlatformAlreadyActive)
	count, err := svc.entClient.UserSubscription.Query().Where(usersubscription.UserIDEQ(user.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	saved, err := svc.entClient.PaymentOrder.Get(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusFailed, saved.Status)
	require.NotNil(t, saved.PaidAt)
	sub, err := repo.GetByUserIDAndGroupID(ctx, user.ID, plan.GroupID)
	require.NoError(t, err)
	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, first.ID))
	after, err := repo.GetByUserIDAndGroupID(ctx, user.ID, plan.GroupID)
	require.NoError(t, err)
	require.True(t, sub.ExpiresAt.Equal(after.ExpiresAt))
}

func TestFreshPlatformPolicyRechecksAfterQuote(t *testing.T) {
	ctx := context.Background()
	svc, plan, user, _ := newOneTimePolicyTestService(t)
	quote, err := svc.GetSubscriptionCheckout(ctx, user.ID, plan.ID, 0, "alipay")
	require.NoError(t, err)
	_, err = svc.entClient.UserSubscription.Create().SetUserID(user.ID).SetGroupID(plan.GroupID).SetStartsAt(time.Now()).SetExpiresAt(time.Now().Add(time.Hour)).SetStatus(SubscriptionStatusActive).Save(ctx)
	require.NoError(t, err)
	_, err = svc.createOrderInTx(ctx, CreateOrderRequest{UserID: user.ID, OrderType: payment.OrderTypeSubscription, ExpectedTotalCents: &quote.TotalCents}, user, plan, &PaymentConfig{}, plan.Price, 1305, 0, 1305, 0, nil, nil)
	require.ErrorIs(t, err, ErrSubscriptionPlatformAlreadyActive)
}

type oneTimeTestLoadBalancer struct {
	selection *payment.InstanceSelection
	amount    float64
}

func (b *oneTimeTestLoadBalancer) GetInstanceConfig(context.Context, int64) (map[string]string, error) {
	return b.selection.Config, nil
}
func (b *oneTimeTestLoadBalancer) SelectInstance(_ context.Context, _ string, _ payment.PaymentType, _ payment.Strategy, amount float64) (*payment.InstanceSelection, error) {
	b.amount = amount
	return b.selection, nil
}

func newOneTimePolicyTestService(t *testing.T) (*PaymentService, *dbent.SubscriptionPlan, *User, *oneTimeTestLoadBalancer) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	group, err := client.Group.Create().SetName("payment policy").SetPlatform("anthropic").SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetName("large subscription").SetGroupID(group.ID).SetPrice(195.18).SetFixedPriceCny(1305).SetValidityDays(30).SetValidityUnit("day").SetForSale(true).SetInstallmentScheduleCents([]int64{80000, 50500}).Save(ctx)
	require.NoError(t, err)
	buyer, err := client.User.Create().SetEmail("one-time@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	user := &User{ID: buyer.ID, Email: buyer.Email, Username: "buyer", Role: "admin", Status: payment.EntityStatusActive}
	lb := &oneTimeTestLoadBalancer{selection: &payment.InstanceSelection{ProviderKey: payment.TypeEasyPay, PaymentMode: "popup", SupportedTypes: "alipay", Config: map[string]string{"pid": "1", "pkey": "test", "apiBase": "https://gateway.invalid", "notifyUrl": "https://example.com/notify", "returnUrl": "https://example.com/return", "paymentMode": "popup"}}}
	cfg := &PaymentConfigService{entClient: client, settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true", SettingSubscriptionUSDToCNYRate: "6.6863", SettingRechargeFeeRate: "2"}}}
	svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{getByIDUser: user}, configService: cfg, groupRepo: &subscriptionGroupRepoStub{group: &Group{ID: group.ID, Platform: "anthropic", Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription}}, loadBalancer: lb}
	return svc, plan, user, lb
}

func TestFreshSubscriptionFullFixedPriceQuoteAndOrderAbove1000(t *testing.T) {
	ctx := context.Background()
	svc, plan, user, lb := newOneTimePolicyTestService(t)
	quote, err := svc.GetSubscriptionCheckout(ctx, user.ID, plan.ID, 0, "alipay")
	require.NoError(t, err)
	require.Equal(t, int64(130500), quote.TotalCents)
	require.Equal(t, 1305.0, quote.NextAmount)
	req := CreateOrderRequest{UserID: user.ID, PlanID: plan.ID, OrderType: payment.OrderTypeSubscription, PaymentType: "alipay", Amount: 800, ExpectedTotalCents: &quote.TotalCents}
	stale := quote.TotalCents - 1
	req.ExpectedTotalCents = &stale
	_, err = svc.CreateOrder(ctx, req)
	require.Equal(t, "QUOTE_CHANGED", infraerrors.Reason(err))
	count, err := svc.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	req.ExpectedTotalCents = &quote.TotalCents
	resp, err := svc.CreateOrder(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1305.0, lb.amount)
	require.Equal(t, 1305.0, resp.PayAmount)
	require.Equal(t, plan.Price, resp.Amount)
	require.Zero(t, resp.PaymentSessionID)
	require.Zero(t, resp.InstallmentNumber)
	parsed, err := url.Parse(resp.PayURL)
	require.NoError(t, err)
	require.Equal(t, "1305.00", parsed.Query().Get("money"))
	order, err := svc.entClient.PaymentOrder.Get(ctx, resp.OrderID)
	require.NoError(t, err)
	require.Equal(t, 195.18, order.Amount)
	require.Zero(t, order.FeeRate)
	require.Nil(t, order.PaymentSessionID)
	require.Nil(t, order.InstallmentNumber)
	count, err = svc.entClient.PaymentSession.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestFreshSubscriptionFXQuoteMatchesChargeAndIgnoresLegacySchedule(t *testing.T) {
	ctx := context.Background()
	svc, plan, user, _ := newOneTimePolicyTestService(t)
	_, err := svc.entClient.SubscriptionPlan.UpdateOneID(plan.ID).SetFixedPriceCny(0).Save(ctx)
	require.NoError(t, err)
	quote, err := svc.GetSubscriptionCheckout(ctx, user.ID, plan.ID, 0, "alipay")
	require.NoError(t, err)
	_, expected, err := calculateCreateOrderPayAmountForOrderType(plan.Price, 2, "CNY", payment.OrderTypeSubscription, 6.6863)
	require.NoError(t, err)
	require.Equal(t, expected, quote.NextAmount)
	require.Greater(t, quote.NextAmount, 1000.0)
	resp, err := svc.CreateOrder(ctx, CreateOrderRequest{UserID: user.ID, PlanID: plan.ID, OrderType: payment.OrderTypeSubscription, PaymentType: "alipay", ExpectedTotalCents: &quote.TotalCents})
	require.NoError(t, err)
	require.Equal(t, quote.NextAmount, resp.PayAmount)
	require.Equal(t, plan.Price, resp.Amount)
	_, _, err = subscriptionOneTimePayAmount(&dbent.SubscriptionPlan{Price: 195.18, FixedPriceCny: 1305}, &PaymentConfig{RechargeFeeRate: 2, SubscriptionUSDToCNYRate: 6.6863}, "USD", nil)
	require.Equal(t, "PAYMENT_SESSION_CURRENCY", infraerrors.Reason(err))
}

func TestPaymentPlatformPolicyActiveExpiredSuspendedDeletedAndSameGroup(t *testing.T) {
	for _, state := range []string{"active_same_group", "active_other_group", "expired", "suspended", "deleted", "other_platform"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			svc, plan, user, _ := newOneTimePolicyTestService(t)
			gid := plan.GroupID
			if state != "active_same_group" {
				platform := "anthropic"
				if state == "other_platform" {
					platform = "openai"
				}
				g, err := svc.entClient.Group.Create().SetName("existing group").SetPlatform(platform).SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
				require.NoError(t, err)
				gid = g.ID
			}
			build := svc.entClient.UserSubscription.Create().SetUserID(user.ID).SetGroupID(gid).SetStartsAt(time.Now().Add(-time.Hour)).SetExpiresAt(time.Now().Add(time.Hour)).SetStatus(SubscriptionStatusActive)
			if state == "expired" {
				build.SetExpiresAt(time.Now().Add(-time.Second))
			}
			if state == "suspended" {
				build.SetStatus(SubscriptionStatusSuspended)
			}
			if state == "deleted" {
				build.SetDeletedAt(time.Now())
			}
			_, err := build.Save(ctx)
			require.NoError(t, err)
			blocked := state == "active_same_group" || state == "active_other_group"
			_, err = svc.validateSubOrder(ctx, CreateOrderRequest{UserID: user.ID, PlanID: plan.ID})
			if blocked {
				require.ErrorIs(t, err, ErrSubscriptionPlatformAlreadyActive)
			} else {
				require.NoError(t, err)
			}
			plans, err := svc.ListUserSubscriptionPlans(ctx, user.ID)
			require.NoError(t, err)
			if blocked {
				require.Empty(t, plans)
			} else {
				require.Len(t, plans, 1)
			}
			_, err = svc.createOrderInTx(ctx, CreateOrderRequest{UserID: user.ID, PlanID: plan.ID, OrderType: payment.OrderTypeSubscription}, user, plan, &PaymentConfig{}, plan.Price, 1305, 0, 1305, 0, nil, nil)
			if blocked {
				require.ErrorIs(t, err, ErrSubscriptionPlatformAlreadyActive)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFreshFulfillmentPlatformConflictAndOwnOrderRetry(t *testing.T) {
	ctx := context.Background()
	svc, plan, user, _ := newOneTimePolicyTestService(t)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, svc.entClient, OrderStatusPaid, time.Now())
	order, err := svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetUserID(user.ID).SetSubscriptionGroupID(plan.GroupID).Save(ctx)
	require.NoError(t, err)
	sub, err := svc.entClient.UserSubscription.Create().SetUserID(user.ID).SetGroupID(plan.GroupID).SetStartsAt(time.Now()).SetExpiresAt(time.Now().Add(time.Hour)).SetStatus(SubscriptionStatusActive).Save(ctx)
	require.NoError(t, err)
	repo := newSubscriptionUserSubRepoStub()
	svc.subscriptionSvc = NewSubscriptionService(svc.groupRepo, repo, nil, nil, nil)
	require.ErrorIs(t, svc.ExecuteSubscriptionFulfillment(ctx, order.ID), ErrSubscriptionPlatformAlreadyActive)
	saved, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusFailed, saved.Status)
	require.NotNil(t, saved.PaidAt)
	// A committed own-order audit is checked before the platform rule.
	_, err = svc.entClient.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("SUBSCRIPTION_ASSIGNED").SetOperator("system").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.RetryFulfillment(ctx, order.ID))
	after, err := svc.entClient.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, after.ExpiresAt.Equal(sub.ExpiresAt))
	require.Zero(t, repo.createCalls)
}
