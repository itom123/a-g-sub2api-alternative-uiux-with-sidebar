//go:build unit

package service

import (
	"context"
	"math"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func newInstallmentTestPurchase(t *testing.T, target float64) (*PaymentService, *dbent.PaymentSession, *dbent.PaymentOrder, *subscriptionUserSubRepoStub, *subscriptionGroupRepoStub) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
	first := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPending, time.Now())
	session, err := client.PaymentSession.Create().SetUserID(first.UserID).SetPlanID(100).SetSubscriptionGroupID(7).SetSubscriptionDays(30).SetBaseAmount(195).SetTargetAmount(target).Save(ctx)
	require.NoError(t, err)
	first, err = client.PaymentOrder.UpdateOneID(first.ID).SetPaymentSessionID(session.ID).SetInstallmentNumber(1).SetPayAmount(800).ClearPaidAt().Save(ctx)
	require.NoError(t, err)
	subRepo := newSubscriptionUserSubRepoStub()
	groupRepo := &subscriptionGroupRepoStub{group: &Group{ID: 7, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription}}
	svc := &PaymentService{entClient: client, groupRepo: groupRepo, subscriptionSvc: NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)}
	return svc, session, first, subRepo, groupRepo
}

func addTestInstallment(t *testing.T, svc *PaymentService, session *dbent.PaymentSession, amount float64) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, svc.entClient, OrderStatusPending, time.Now())
	order, err := svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetUserID(session.UserID).SetPaymentSessionID(session.ID).SetInstallmentNumber(2).SetPayAmount(amount).ClearPaidAt().Save(ctx)
	require.NoError(t, err)
	return order
}

func TestInstallments800Then505AllocateOnceAndRecoverDuplicates(t *testing.T) {
	ctx := context.Background()
	svc, session, first, subs, _ := newInstallmentTestPurchase(t, 1305)
	require.NoError(t, svc.toPaid(ctx, first, "first", 800, "test"))
	require.NoError(t, svc.toPaid(ctx, first, "first", 800, "test"))
	saved, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 800.0, saved.PaidAmount)
	require.Equal(t, "PENDING", saved.Status)
	_, err = subs.GetByUserIDAndGroupID(ctx, session.UserID, 7)
	require.ErrorIs(t, err, ErrSubscriptionNotFound)
	// Recover a crash after PAID, before accounting/completion; do not allocate early.
	_, err = svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetStatus(OrderStatusPaid).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.RetryFulfillment(ctx, first.ID))
	second := addTestInstallment(t, svc, session, 505)
	require.NoError(t, svc.toPaid(ctx, second, "tail", 505, "test"))
	sub, err := subs.GetByUserIDAndGroupID(ctx, session.UserID, 7)
	require.NoError(t, err)
	expires := sub.ExpiresAt
	require.NoError(t, svc.toPaid(ctx, second, "tail", 505, "test"))
	require.NoError(t, svc.toPaid(ctx, first, "first", 800, "test"))
	assertPaymentSubscriptionExpiry(t, subs, second, expires)
	saved, err = svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 1305.0, saved.PaidAmount)
	require.Equal(t, "FULFILLED", saved.Status)
	require.Equal(t, second.ID, *saved.AllocationOrderID)
	summaries, err := svc.GetUserPaymentSessions(ctx, session.UserID)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Len(t, summaries[0].Orders, 2)
}

func TestPaidInstallmentAllocationFailureRetriesWithoutAnotherPayment(t *testing.T) {
	ctx := context.Background()
	svc, session, first, subs, group := newInstallmentTestPurchase(t, 800)
	group.group.Status = "disabled"
	require.Error(t, svc.toPaid(ctx, first, "paid", 800, "test"))
	saved, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 800.0, saved.PaidAmount)
	require.Equal(t, "ALLOCATION_FAILED", saved.Status)
	group.group.Status = payment.EntityStatusActive
	require.NoError(t, svc.RetryFulfillment(ctx, first.ID))
	sub, err := subs.GetByUserIDAndGroupID(ctx, session.UserID, 7)
	require.NoError(t, err)
	expires := sub.ExpiresAt
	require.NoError(t, svc.toPaid(ctx, first, "paid", 800, "test"))
	assertPaymentSubscriptionExpiry(t, subs, first, expires)
}

func TestInstallmentCannotFulfillUnpaidOrder(t *testing.T) {
	ctx := context.Background()
	svc, _, first, _, _ := newInstallmentTestPurchase(t, 800)
	_, err := svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetStatus(OrderStatusFailed).Save(ctx)
	require.NoError(t, err)
	require.ErrorContains(t, svc.RetryFulfillment(ctx, first.ID), "not paid")
}

func TestFixedCNYPriceAndInvalidInstallmentInput(t *testing.T) {
	svc := &PaymentService{}
	ctx := context.Background()
	installment, err := svc.prepareSubscriptionInstallment(ctx, CreateOrderRequest{Amount: 800}, &dbent.SubscriptionPlan{Price: 195.18, FixedPriceCny: 1305}, &PaymentConfig{SubscriptionUSDToCNYRate: 6.6863, RechargeFeeRate: 2}, "CNY")
	require.NoError(t, err)
	require.Equal(t, 1305.0, installment.Target)
	require.Equal(t, 800.0, installment.Amount)
	_, err = svc.prepareSubscriptionInstallment(ctx, CreateOrderRequest{}, &dbent.SubscriptionPlan{Price: 195, FixedPriceCny: 1305}, &PaymentConfig{}, "USD")
	require.Error(t, err)
	for _, bad := range []float64{math.NaN(), math.Inf(1)} {
		_, err = chooseSubscriptionInstallmentAmount(1305, bad)
		require.Error(t, err)
	}
}

func TestInstallmentCreationRejectsOverlappingOrdersAndStaleAmounts(t *testing.T) {
	ctx := context.Background()
	svc, session, first, _, _ := newInstallmentTestPurchase(t, 1305)
	buyer := &User{ID: session.UserID, Email: first.UserEmail, Username: first.UserName}
	req := CreateOrderRequest{UserID: session.UserID, PlanID: 100, PaymentSessionID: session.ID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeAlipay, ClientIP: "127.0.0.1", SrcHost: "example.com"}
	plan := &dbent.SubscriptionPlan{ID: 100, GroupID: 7, ValidityDays: 30, ValidityUnit: "day", Price: 195}
	prepared := &subscriptionInstallment{SessionID: session.ID, Target: 1305, BaseAmount: 195, Amount: 800, Number: 1, Currency: "CNY"}
	_, err := svc.createOrderInTx(ctx, req, buyer, plan, &PaymentConfig{MaxPendingOrders: 5}, 119.54, 800, 0, 800, 0, nil, prepared)
	require.ErrorContains(t, err, "INSTALLMENT_PENDING")
	_, err = svc.entClient.PaymentOrder.UpdateOneID(first.ID).SetStatus(OrderStatusCompleted).Save(ctx)
	require.NoError(t, err)
	_, err = svc.entClient.PaymentSession.UpdateOneID(session.ID).SetPaidAmount(800).Save(ctx)
	require.NoError(t, err)
	_, err = svc.createOrderInTx(ctx, req, buyer, plan, &PaymentConfig{MaxPendingOrders: 5}, 119.54, 800, 0, 800, 0, nil, prepared)
	require.ErrorContains(t, err, "PAYMENT_SESSION_CHANGED")
	req.PaymentSessionID = 0
	prepared.SessionID = 0
	_, err = svc.createOrderInTx(ctx, req, buyer, plan, &PaymentConfig{MaxPendingOrders: 5}, 119.54, 800, 0, 800, 0, nil, prepared)
	require.ErrorContains(t, err, "PAYMENT_SESSION_EXISTS")
}

func TestFixedCNYPlanPersistsAndOpenPurchaseSurvivesTakingItOffSale(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	group, err := client.Group.Create().SetName("campus").SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	cfg := &PaymentConfigService{entClient: client}
	plan, err := cfg.CreatePlan(ctx, CreatePlanRequest{GroupID: group.ID, Name: "Campus", Price: 195.18, FixedPriceCNY: 1305, ValidityDays: 30, ValidityUnit: "day", ForSale: true})
	require.NoError(t, err)
	fixed := 783.0
	plan, err = cfg.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{FixedPriceCNY: &fixed})
	require.NoError(t, err)
	require.Equal(t, 783.0, plan.FixedPriceCny)
	buyer, err := client.User.Create().SetEmail("campus@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentSession.Create().SetUserID(buyer.ID).SetPlanID(plan.ID).SetSubscriptionGroupID(group.ID).SetSubscriptionDays(30).SetBaseAmount(195.18).SetTargetAmount(1305).Save(ctx)
	require.NoError(t, err)
	off := false
	_, err = cfg.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{ForSale: &off})
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, configService: cfg}
	plans, err := svc.ListUserSubscriptionPlans(ctx, buyer.ID)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, plan.ID, plans[0].ID)
	require.ErrorContains(t, cfg.DeletePlan(ctx, plan.ID), "unfinished subscription payment")
}
