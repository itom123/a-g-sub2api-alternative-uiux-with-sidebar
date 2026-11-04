//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type sessionCancelTestProvider struct {
	paymentOrderLifecycleQueryProvider
	queryErr  error
	cancelErr error
	onQuery   func(context.Context)
	onCancel  func(context.Context)
	bounded   bool
}

func (p *sessionCancelTestProvider) QueryOrder(ctx context.Context, ref string) (*payment.QueryOrderResponse, error) {
	_, p.bounded = ctx.Deadline()
	if p.onQuery != nil {
		p.onQuery(ctx)
	}
	if p.queryErr != nil {
		p.queryCalls++
		return nil, p.queryErr
	}
	return p.paymentOrderLifecycleQueryProvider.QueryOrder(ctx, ref)
}
func (p *sessionCancelTestProvider) CancelPayment(ctx context.Context, ref string) error {
	p.cancelCalls++
	p.lastCancelTradeNo = ref
	if p.onCancel != nil {
		p.onCancel(ctx)
	}
	return p.cancelErr
}
func registerSessionCancelProvider(svc *PaymentService, p payment.Provider) {
	svc.registry = payment.NewRegistry()
	svc.registry.Register(p)
	svc.providersLoaded = true
}

func TestCancelSubscriptionSessionChecksPaidAndProcessingSiblingsFirst(t *testing.T) {
	for _, state := range []string{"paid_at", OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted, "session_partial"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			svc, session, _, _, _ := newInstallmentTestPurchase(t, 1305)
			second := addTestInstallment(t, svc, session, 505)
			update := svc.entClient.PaymentOrder.UpdateOneID(second.ID)
			if state == "paid_at" {
				update.SetPaidAt(time.Now())
			} else if state != "session_partial" {
				update.SetStatus(state)
			}
			_, err := update.Save(ctx)
			require.NoError(t, err)
			if state == "session_partial" {
				_, err = svc.entClient.PaymentSession.UpdateOneID(session.ID).SetPaidAmount(1).Save(ctx)
				require.NoError(t, err)
			}
			p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}}
			registerSessionCancelProvider(svc, p)
			err = svc.CancelSubscriptionSession(ctx, session.UserID, session.ID)
			require.Equal(t, "PAYMENT_IN_PROGRESS", infraerrors.Reason(err))
			require.Zero(t, p.cancelCalls)
			require.Zero(t, p.queryCalls)
			saved, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
			require.NoError(t, err)
			require.Equal(t, "PENDING", saved.Status)
		})
	}
}

func TestCancelSubscriptionSessionProvesClosureForEveryLocalStatusAndIsIdempotent(t *testing.T) {
	for _, status := range []string{OrderStatusPending, OrderStatusExpired, OrderStatusFailed, OrderStatusCancelled} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			svc, session, order, _, _ := newInstallmentTestPurchase(t, 1305)
			_, err := svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(status).SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
			require.NoError(t, err)
			p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}}
			registerSessionCancelProvider(svc, p)
			require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
			require.Equal(t, 1, p.queryCalls)
			require.Equal(t, 1, p.cancelCalls)
			require.True(t, p.bounded)
			saved, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, saved.ProviderClosed)
			require.Equal(t, OrderStatusCancelled, saved.Status)
			require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
			require.Equal(t, 1, p.queryCalls)
			require.Equal(t, 1, p.cancelCalls)
			require.Error(t, svc.CancelSubscriptionSession(ctx, session.UserID+1, session.ID))
			quote, err := svc.GetSubscriptionCheckout(ctx, session.UserID, session.PlanID, session.ID, "alipay")
			require.NoError(t, err)
			require.Nil(t, quote.PendingOrder)
			require.Zero(t, quote.NextAmount)
		})
	}
}

func TestCancelSubscriptionSessionUnknownOrProcessingNeverCloses(t *testing.T) {
	for _, state := range []string{"query_error", "nil_response", "cancel_error", "processing", "stripe_processing", "unsupported_status", "provider_create"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			svc, session, order, _, _ := newInstallmentTestPurchase(t, 1305)
			_, err := svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusExpired).SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
			require.NoError(t, err)
			p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}}
			switch state {
			case "query_error":
				p.queryErr = errors.New("offline")
			case "nil_response":
				p.resp = nil
			case "cancel_error":
				p.cancelErr = errors.New("not closed")
			case "processing":
				p.resp.Metadata = map[string]string{"trade_state": "USERPAYING"}
			case "stripe_processing":
				p.resp.Metadata = map[string]string{"status": "processing"}
			case "unsupported_status":
				p.resp.Status = "unknown"
			case "provider_create":
				_, err = svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetFailedReason(paymentProviderCreateInProgress).Save(ctx)
				require.NoError(t, err)
			}
			registerSessionCancelProvider(svc, p)
			err = svc.CancelSubscriptionSession(ctx, session.UserID, session.ID)
			require.Error(t, err)
			saved, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
			require.NoError(t, err)
			require.NotEqual(t, paymentSessionCancelled, saved.Status)
			if state != "cancel_error" {
				require.Zero(t, p.cancelCalls)
			}
			fresh, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.False(t, fresh.ProviderClosed)
		})
	}
}

func TestCancelSubscriptionSessionQueriesAllBeforeCancellingAndPreservesUpstreamPaid(t *testing.T) {
	ctx := context.Background()
	svc, session, _, _, _ := newInstallmentTestPurchase(t, 1305)
	second := addTestInstallment(t, svc, session, 505)
	p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{responses: []*payment.QueryOrderResponse{{Status: payment.ProviderStatusPending}, {Status: payment.ProviderStatusPaid, Amount: 505, TradeNo: "paid-second"}}}}
	registerSessionCancelProvider(svc, p)
	err := svc.CancelSubscriptionSession(ctx, session.UserID, session.ID)
	require.Equal(t, "PAYMENT_IN_PROGRESS", infraerrors.Reason(err))
	require.Zero(t, p.cancelCalls)
	saved, err := svc.entClient.PaymentOrder.Get(ctx, second.ID)
	require.NoError(t, err)
	require.NotNil(t, saved.PaidAt)
	bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 505.0, bill.PaidAmount)
	require.Equal(t, "PENDING", bill.Status)
}

func TestCancelSubscriptionSessionFinalRecheckProtectsConcurrentPayment(t *testing.T) {
	ctx := context.Background()
	svc, session, order, _, _ := newInstallmentTestPurchase(t, 1305)
	p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}}
	p.onCancel = func(callCtx context.Context) {
		require.NoError(t, svc.toPaid(callCtx, order, "race-paid", 800, "alipay"))
	}
	registerSessionCancelProvider(svc, p)
	err := svc.CancelSubscriptionSession(ctx, session.UserID, session.ID)
	require.Equal(t, "PAYMENT_IN_PROGRESS", infraerrors.Reason(err))
	bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 800.0, bill.PaidAmount)
	require.Equal(t, "PENDING", bill.Status)
}

func TestCancelSubscriptionSessionLateCallbackPersistsReviewMoneyWithoutAllocation(t *testing.T) {
	ctx := context.Background()
	svc, session, order, subs, _ := newInstallmentTestPurchase(t, 800)
	_, err := svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetProviderClosed(true).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
	for i := 0; i < 2; i++ {
		require.NoError(t, svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{OrderID: order.OutTradeNo, TradeNo: "late-money", Amount: 800, Status: payment.NotificationStatusSuccess}, "alipay"))
	}
	saved, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.NotNil(t, saved.PaidAt)
	require.Equal(t, 800.0, saved.PayAmount)
	require.Equal(t, "late-money", saved.PaymentTradeNo)
	require.Equal(t, OrderStatusFailed, saved.Status)
	require.Equal(t, paymentSessionReviewNeeded, *saved.FailedReason)
	bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, paymentSessionCancelled, bill.Status)
	require.Equal(t, 800.0, bill.PaidAmount)
	require.Nil(t, bill.AllocationOrderID)
	require.ErrorContains(t, svc.RetryFulfillment(ctx, order.ID), "PAYMENT_SESSION_CLOSED")
	require.Zero(t, subs.createCalls)
	count, err := svc.entClient.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("PAYMENT_AFTER_SESSION_CANCELLED")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestCancelSubscriptionSessionFencesNewOrdersDuringProviderCalls(t *testing.T) {
	ctx := context.Background()
	svc, session, _, _, _ := newInstallmentTestPurchase(t, 1305)
	plan := &dbent.SubscriptionPlan{ID: session.PlanID, GroupID: session.SubscriptionGroupID, Price: 195, ValidityDays: 30, ValidityUnit: "day"}
	p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}}
	p.onQuery = func(callCtx context.Context) {
		fresh, err := svc.entClient.PaymentSession.Get(callCtx, session.ID)
		require.NoError(t, err)
		require.Equal(t, paymentSessionCancelling, fresh.Status)
		buyer := &User{ID: session.UserID, Email: "fenced@example.com"}
		_, err = svc.createOrderInTx(callCtx, CreateOrderRequest{UserID: session.UserID, OrderType: payment.OrderTypeSubscription}, buyer, plan, &PaymentConfig{}, 195, 1305, 0, 1305, 0, nil, nil)
		require.Equal(t, "PAYMENT_SESSION_EXISTS", infraerrors.Reason(err))
		_, err = svc.createOrderInTx(callCtx, CreateOrderRequest{UserID: session.UserID, PaymentSessionID: session.ID, OrderType: payment.OrderTypeSubscription}, buyer, plan, &PaymentConfig{}, 195, 800, 0, 800, 0, nil, &subscriptionInstallment{SessionID: session.ID, Amount: 800, Target: 1305})
		require.Equal(t, "PAYMENT_SESSION_CHANGED", infraerrors.Reason(err))
	}
	registerSessionCancelProvider(svc, p)
	require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
}

func TestCancelSubscriptionSessionLateMismatchedMoneyIsVisible(t *testing.T) {
	ctx := context.Background()
	svc, session, order, subs, _ := newInstallmentTestPurchase(t, 800)
	_, err := svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetProviderClosed(true).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
	require.NoError(t, svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{OrderID: order.OutTradeNo, TradeNo: "unexpected-money", Amount: 799, Status: payment.NotificationStatusSuccess}, "alipay"))
	fresh, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, 799.0, fresh.PayAmount)
	require.NotNil(t, fresh.PaidAt)
	bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, 799.0, bill.PaidAmount)
	require.Equal(t, paymentSessionCancelled, bill.Status)
	require.Zero(t, subs.createCalls)
}

func TestLegacySessionCleanupDryRunHasNoProviderOrDatabaseMutation(t *testing.T) {
	ctx := context.Background()
	svc, session, _, _, _ := newInstallmentTestPurchase(t, 1305)
	p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}}
	registerSessionCancelProvider(svc, p)
	results, err := svc.CleanupUnpaidLegacySessions(ctx, false, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "candidate", results[0].Status)
	require.Zero(t, p.queryCalls)
	require.Zero(t, p.cancelCalls)
	bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, "PENDING", bill.Status)
	results, err = svc.CleanupUnpaidLegacySessions(ctx, true, 10)
	require.NoError(t, err)
	require.Equal(t, "cancelled", results[0].Status)
}

// queryOnlyTestProvider models an aggregator such as EasyPay: it can query an
// order but has no upstream close API (it is not a payment.CancelableProvider).
type queryOnlyTestProvider struct {
	inner *paymentOrderLifecycleQueryProvider
}

func (p queryOnlyTestProvider) Name() string        { return p.inner.Name() }
func (p queryOnlyTestProvider) ProviderKey() string { return p.inner.ProviderKey() }
func (p queryOnlyTestProvider) SupportedTypes() []payment.PaymentType {
	return p.inner.SupportedTypes()
}
func (p queryOnlyTestProvider) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	return p.inner.CreatePayment(ctx, req)
}
func (p queryOnlyTestProvider) QueryOrder(ctx context.Context, ref string) (*payment.QueryOrderResponse, error) {
	return p.inner.QueryOrder(ctx, ref)
}
func (p queryOnlyTestProvider) VerifyNotification(ctx context.Context, body string, headers map[string]string) (*payment.PaymentNotification, error) {
	return p.inner.VerifyNotification(ctx, body, headers)
}
func (p queryOnlyTestProvider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	return p.inner.Refund(ctx, req)
}

func TestCancelSubscriptionSessionWithoutProviderCloseAPIClosesLocallyAfterUnpaidProof(t *testing.T) {
	for _, status := range []string{payment.ProviderStatusPending, payment.ProviderStatusFailed} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			svc, session, order, subs, _ := newInstallmentTestPurchase(t, 800)
			_, err := svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetExpiresAt(time.Now().Add(-4 * time.Hour)).Save(ctx)
			require.NoError(t, err)
			inner := &paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: status}}
			registerSessionCancelProvider(svc, queryOnlyTestProvider{inner: inner})
			_, cancelable := payment.Provider(queryOnlyTestProvider{inner: inner}).(payment.CancelableProvider)
			require.False(t, cancelable)

			require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
			require.Equal(t, 1, inner.queryCalls)
			require.Zero(t, inner.cancelCalls)
			saved, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusCancelled, saved.Status)
			require.False(t, saved.ProviderClosed, "a local close must not claim the gateway closed the order")
			bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
			require.NoError(t, err)
			require.Equal(t, paymentSessionCancelled, bill.Status)
			require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
			require.Equal(t, 1, inner.queryCalls)

			// Money that still arrives for the locally closed order is kept for review, never allocated.
			require.NoError(t, svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{OrderID: order.OutTradeNo, TradeNo: "late-easypay", Amount: 800, Status: payment.NotificationStatusSuccess}, "alipay"))
			late, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.NotNil(t, late.PaidAt)
			require.Equal(t, OrderStatusFailed, late.Status)
			require.Equal(t, paymentSessionReviewNeeded, *late.FailedReason)
			require.Zero(t, subs.createCalls)
		})
	}
}

func TestCancelSubscriptionSessionWithoutProviderCloseAPIStillRefusesPaidOrUnknown(t *testing.T) {
	for _, state := range []string{"paid", "processing", "query_error"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			svc, session, order, _, _ := newInstallmentTestPurchase(t, 800)
			inner := &paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}
			var p payment.Provider = queryOnlyTestProvider{inner: inner}
			switch state {
			case "paid":
				inner.resp = &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: 800, TradeNo: "paid-easypay"}
			case "processing":
				inner.resp.Metadata = map[string]string{"status": "processing"}
			case "query_error":
				p = &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: *inner, queryErr: errors.New("gateway offline")}
			}
			registerSessionCancelProvider(svc, p)
			err := svc.CancelSubscriptionSession(ctx, session.UserID, session.ID)
			require.Error(t, err)
			bill, err := svc.entClient.PaymentSession.Get(ctx, session.ID)
			require.NoError(t, err)
			require.NotEqual(t, paymentSessionCancelled, bill.Status)
			saved, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.NotEqual(t, OrderStatusCancelled, saved.Status)
		})
	}
}

func TestCancelSubscriptionSessionAcceptsAlreadyClosedUpstreamTrade(t *testing.T) {
	ctx := context.Background()
	svc, session, order, _, _ := newInstallmentTestPurchase(t, 800)
	// Alipay reports TRADE_CLOSED as failed, then rejects TradeClose on it.
	p := &sessionCancelTestProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusFailed}}, cancelErr: errors.New("ACQ.TRADE_STATUS_ERROR")}
	registerSessionCancelProvider(svc, p)
	require.NoError(t, svc.CancelSubscriptionSession(ctx, session.UserID, session.ID))
	require.Equal(t, 1, p.cancelCalls)
	saved, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, saved.ProviderClosed)
	require.Equal(t, OrderStatusCancelled, saved.Status)
}
