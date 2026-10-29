//go:build unit

package service

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPrivateScheduledPlanEligibilityAndValidation(t *testing.T) {
	ctx := context.Background()
	plan := &dbent.SubscriptionPlan{EligibleUserIds: []int64{42}}
	svc := &PaymentService{userRepo: subscriptionAccessUserRepo{user: &User{ID: 7, Role: "user"}}}
	require.False(t, svc.subscriptionPlanAllowed(ctx, 7, plan))
	svc.userRepo = subscriptionAccessUserRepo{user: &User{ID: 42, Role: "user"}}
	require.True(t, svc.subscriptionPlanAllowed(ctx, 42, plan))
	svc.userRepo = subscriptionAccessUserRepo{user: &User{ID: 1, Role: "admin"}}
	require.True(t, svc.subscriptionPlanAllowed(ctx, 1, plan))
	require.NoError(t, validatePlanSchedule(13, []int64{800, 500}, []int64{42}))
	require.Error(t, validatePlanSchedule(13, []int64{800, 600}, []int64{42}))
	require.Error(t, validatePlanSchedule(13, nil, []int64{-1}))
}

func TestCheckoutScheduleAndStaleQuote(t *testing.T) {
	svc := &PaymentService{}
	plan := &dbent.SubscriptionPlan{Price: 13, FixedPriceCny: 13, InstallmentScheduleCents: []int64{800, 500}}
	expected := int64(1300)
	part, err := svc.prepareSubscriptionInstallment(context.Background(), CreateOrderRequest{ExpectedTotalCents: &expected}, plan, &PaymentConfig{MinAmount: 1}, "CNY")
	require.NoError(t, err)
	require.Equal(t, 8.0, part.Amount)
	next, err := scheduledInstallment(plan.InstallmentScheduleCents, 8)
	require.NoError(t, err)
	require.Equal(t, 5.0, next)
	_, err = svc.prepareSubscriptionInstallment(context.Background(), CreateOrderRequest{Amount: 13}, plan, &PaymentConfig{MinAmount: 1}, "CNY")
	require.ErrorContains(t, err, "INSTALLMENT_SCHEDULE")
	expected = 1400
	_, err = svc.prepareSubscriptionInstallment(context.Background(), CreateOrderRequest{ExpectedTotalCents: &expected}, plan, &PaymentConfig{}, "CNY")
	require.ErrorContains(t, err, "QUOTE_CHANGED")
}

func TestThirteenYuanInstallmentsAndLateExcessKeepSingleAllocation(t *testing.T) {
	ctx := context.Background()
	svc, session, first, subs, _ := newInstallmentTestPurchase(t, 13)
	first, err := svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetPayAmount(8).SetStatus(OrderStatusExpired).SetExpiresAt(time.Now().Add(-time.Hour)).SetUpdatedAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.toPaid(ctx, first, "late-first", 8, "test"))
	saved, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 8.0, saved.PaidAmount)
	require.Equal(t, "PENDING", saved.Status)
	_, err = subs.GetByUserIDAndGroupID(ctx, session.UserID, 7)
	require.ErrorIs(t, err, ErrSubscriptionNotFound)
	second := addTestInstallment(t, svc, session, 5)
	require.NoError(t, svc.toPaid(ctx, second, "second", 5, "test"))
	sub, err := subs.GetByUserIDAndGroupID(ctx, session.UserID, 7)
	require.NoError(t, err)
	expiry := sub.ExpiresAt
	extra := addTestInstallment(t, svc, session, 8)
	require.NoError(t, svc.toPaid(ctx, extra, "unexpected-late", 8, "test"))
	saved, err = svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 21.0, saved.PaidAmount)
	require.Equal(t, "FULFILLED", saved.Status)
	require.Equal(t, second.ID, *saved.AllocationOrderID)
	assertPaymentSubscriptionExpiry(t, subs, second, expiry)
	history, err := svc.GetUserPaymentSessions(ctx, session.UserID)
	require.NoError(t, err)
	require.Equal(t, 8.0, history[0].OverpaidAmount)
}

func TestHistoryPaginationRetainsOldBills(t *testing.T) {
	ctx := context.Background()
	svc, session, _, _, _ := newInstallmentTestPurchase(t, 13)
	for i := 0; i < 24; i++ {
		_, err := svc.entClient.PaymentSession.Create().SetUserID(session.UserID).SetPlanID(100).SetPlanName("Test bill").SetSubscriptionGroupID(7).SetSubscriptionDays(30).SetBaseAmount(13).SetTargetAmount(13).SetStatus("FULFILLED").Save(ctx)
		require.NoError(t, err)
	}
	first, total, err := svc.GetUserPaymentSessionsPage(ctx, session.UserID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 25, total)
	require.Len(t, first, 20)
	second, total, err := svc.GetUserPaymentSessionsPage(ctx, session.UserID, 2, 20)
	require.NoError(t, err)
	require.Equal(t, 25, total)
	require.Len(t, second, 5)
	require.Equal(t, session.ID, second[4].ID)
}

func TestExpiredUnconfirmedOrderStillBlocksReplacement(t *testing.T) {
	ctx := context.Background()
	svc, session, first, _, _ := newInstallmentTestPurchase(t, 13)
	_, err := svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetPayAmount(8).SetStatus(OrderStatusExpired).SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	req := CreateOrderRequest{UserID: session.UserID, PlanID: 100, PaymentSessionID: session.ID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeAlipay}
	prepared := &subscriptionInstallment{SessionID: session.ID, Target: 13, BaseAmount: 13, Amount: 8, Number: 2, Currency: "CNY"}
	_, err = svc.createOrderInTx(ctx, req, &User{ID: session.UserID, Email: first.UserEmail}, &dbent.SubscriptionPlan{ID: 100, GroupID: 7}, &PaymentConfig{MaxPendingOrders: 5}, 8, 8, 0, 8, 0, nil, prepared)
	require.ErrorContains(t, err, "INSTALLMENT_PENDING")
}

func TestSavedCheckoutReturnsSameOrderAndProtectsOwnership(t *testing.T) {
	ctx := context.Background()
	svc, session, first, _, _ := newInstallmentTestPurchase(t, 13)
	svc.userRepo = subscriptionAccessUserRepo{user: &User{ID: session.UserID, Role: "admin"}}
	svc.configService = &PaymentConfigService{entClient: svc.entClient, settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{}}}
	plan, err := svc.entClient.SubscriptionPlan.Create().SetGroupID(7).SetName("Private test").SetPrice(13).SetFixedPriceCny(13).SetValidityDays(30).SetValidityUnit("days").SetForSale(true).Save(ctx)
	require.NoError(t, err)
	session, err = svc.entClient.PaymentSession.UpdateOneID(session.ID).SetPlanID(plan.ID).Save(ctx)
	require.NoError(t, err)
	_, err = svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetPayAmount(8).SetPayURL("https://gateway.example/existing").SetQrCode("same-payment").SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		quote, err := svc.GetSubscriptionCheckout(ctx, session.UserID, session.PlanID, session.ID, "alipay")
		require.NoError(t, err)
		require.NotNil(t, quote.PendingOrder)
		require.Equal(t, first.ID, quote.PendingOrder.ID)
		require.Equal(t, "same-payment", quote.PendingOrder.QRCode)
		require.False(t, quote.PendingOrder.Checking)
		require.Equal(t, int64(1300), quote.TotalCents)
	}
	count, err := svc.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	_, err = svc.GetSubscriptionCheckout(ctx, session.UserID+1, session.PlanID, session.ID, "alipay")
	require.Error(t, err)
	require.Error(t, svc.ReconcileSubscriptionSession(ctx, session.UserID+1, session.ID))
	_, err = svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	quote, err := svc.GetSubscriptionCheckout(ctx, session.UserID, session.PlanID, session.ID, "alipay")
	require.NoError(t, err)
	require.True(t, quote.PendingOrder.Checking)
	require.Empty(t, quote.PendingOrder.QRCode)
	require.Empty(t, quote.PendingOrder.PayURL)
}

func TestConfirmedPaymentRecoversLegacyFailedInstallment(t *testing.T) {
	ctx := context.Background()
	svc, session, first, _, _ := newInstallmentTestPurchase(t, 13)
	first, err := svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetPayAmount(8).SetStatus(OrderStatusFailed).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.toPaid(ctx, first, "confirmed-after-create-timeout", 8, "test"))
	bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 8.0, bill.PaidAmount)
	require.Equal(t, "PENDING", bill.Status)
}
