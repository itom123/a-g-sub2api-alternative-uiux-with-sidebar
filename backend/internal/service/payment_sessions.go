package service

import (
	"context"
	"fmt"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentsession"
)

// GetUserPaymentSessions returns open or recently failed installment sessions
// so a user can resume a purchase after leaving the checkout page.
func (s *PaymentService) GetUserPaymentSessions(ctx context.Context, userID int64) ([]PaymentSessionSummary, error) {
	// Include every unfinished bill even when it predates the latest history page.
	open, err := s.entClient.PaymentSession.Query().Where(paymentsession.UserIDEQ(userID), paymentsession.StatusIn("PENDING", "PAID", "ALLOCATION_FAILED")).Order(dbent.Desc(paymentsession.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	recent, err := s.entClient.PaymentSession.Query().Where(paymentsession.UserIDEQ(userID)).Order(dbent.Desc(paymentsession.FieldID)).Limit(20).All(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	sessions := make([]*dbent.PaymentSession, 0, len(open)+len(recent))
	for _, session := range append(open, recent...) {
		if !seen[session.ID] {
			seen[session.ID] = true
			sessions = append(sessions, session)
		}
	}
	return s.summarizePaymentSessions(ctx, sessions)
}

func (s *PaymentService) GetUserPaymentSessionsPage(ctx context.Context, userID int64, page, size int) ([]PaymentSessionSummary, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	q := s.entClient.PaymentSession.Query().Where(paymentsession.UserIDEQ(userID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	sessions, err := q.Order(dbent.Desc(paymentsession.FieldCreatedAt), dbent.Desc(paymentsession.FieldID)).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list payment sessions: %w", err)
	}
	result, err := s.summarizePaymentSessions(ctx, sessions)
	return result, total, err
}

func (s *PaymentService) summarizePaymentSessions(ctx context.Context, sessions []*dbent.PaymentSession) ([]PaymentSessionSummary, error) {
	result := make([]PaymentSessionSummary, 0, len(sessions))
	for _, session := range sessions {
		orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.PaymentSessionIDEQ(session.ID)).Order(paymentorder.ByID()).All(ctx)
		if err != nil {
			return nil, err
		}
		history := make([]PaymentSessionOrder, 0, len(orders))
		for _, o := range orders {
			history = append(history, PaymentSessionOrder{CreatedAt: o.CreatedAt, PaidAt: o.PaidAt, OutTradeNo: o.OutTradeNo, ProviderClosed: o.ProviderClosed, ID: o.ID, PayAmount: o.PayAmount, Status: o.Status, ExpiresAt: o.ExpiresAt, InstallmentNumber: o.InstallmentNumber})
		}
		result = append(result, PaymentSessionSummary{
			PlanName: session.PlanName, InstallmentScheduleCents: session.InstallmentScheduleCents, OverpaidAmount: math.Max(0, math.Round((session.PaidAmount-session.TargetAmount)*100)/100), Orders: history, AllocationOrderID: session.AllocationOrderID,
			ID:                  session.ID,
			PlanID:              session.PlanID,
			SubscriptionGroupID: session.SubscriptionGroupID,
			TargetAmount:        session.TargetAmount,
			PaidAmount:          session.PaidAmount,
			Currency:            session.Currency,
			Status:              session.Status,
			AllocationError:     session.AllocationError,
			CreatedAt:           session.CreatedAt,
		})
	}
	return result, nil
}

// Keep an already-started purchase available even if its plan is taken off sale.
// The installment session, rather than a subsequently edited plan, fixes the bill
// and subscription entitlement for that purchase.
func (s *PaymentService) ListUserSubscriptionPlans(ctx context.Context, userID int64) ([]*dbent.SubscriptionPlan, error) {
	plans, err := s.configService.ListPlansForSale(ctx)
	if err != nil {
		return nil, err
	}
	eligible := make([]*dbent.SubscriptionPlan, 0, len(plans))
	for _, plan := range plans {
		if s.subscriptionPlanAllowed(ctx, userID, plan) {
			eligible = append(eligible, plan)
		}
	}
	plans = eligible
	sessions, err := s.entClient.PaymentSession.Query().Where(paymentsession.UserIDEQ(userID), paymentsession.StatusIn("PENDING", "PAID", "ALLOCATION_FAILED")).All(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(plans))
	for _, p := range plans {
		seen[p.ID] = true
	}
	for _, session := range sessions {
		if seen[session.PlanID] {
			continue
		}
		plan, err := s.configService.GetPlan(ctx, session.PlanID)
		if err != nil {
			if !dbent.IsNotFound(err) {
				return nil, err
			}
			plan = &dbent.SubscriptionPlan{ID: session.PlanID, Name: session.PlanName, GroupID: session.SubscriptionGroupID, Price: session.BaseAmount, FixedPriceCny: session.TargetAmount, ValidityDays: session.SubscriptionDays, ValidityUnit: "days", InstallmentScheduleCents: session.InstallmentScheduleCents}
		}
		plans = append(plans, plan)
		seen[plan.ID] = true
	}
	return plans, nil
}

// Checkout quotes and pending payment details are derived from the server bill.
// No provider order is created by viewing or reopening a bill.
type SubscriptionCheckout struct {
	TotalCents   int64                       `json:"total_cents"`
	NextAmount   float64                     `json:"next_amount"`
	PendingOrder *SubscriptionPendingPayment `json:"pending_order,omitempty"`
}
type SubscriptionPendingPayment struct {
	ID          int64     `json:"id"`
	PaymentType string    `json:"payment_type"`
	PayAmount   float64   `json:"pay_amount"`
	Currency    string    `json:"currency"`
	ExpiresAt   time.Time `json:"expires_at"`
	OutTradeNo  string    `json:"out_trade_no"`
	PayURL      string    `json:"pay_url"`
	QRCode      string    `json:"qr_code"`
	QRCodeImg   string    `json:"qr_code_img"`
	Checking    bool      `json:"checking"`
}

func (s *PaymentService) GetSubscriptionCheckout(ctx context.Context, userID, planID, sessionID int64, method string) (*SubscriptionCheckout, error) {
	// A settled bill remains readable after its plan/group is retired.
	if sessionID > 0 {
		session, err := s.entClient.PaymentSession.Get(ctx, sessionID)
		if err != nil || session.UserID != userID || session.PlanID != planID {
			return nil, infraerrors.NotFound("NOT_FOUND", "bill not found")
		}
		if session.Status != "PENDING" {
			return &SubscriptionCheckout{TotalCents: int64(math.Round(session.TargetAmount * 100))}, nil
		}
	}
	req := CreateOrderRequest{UserID: userID, PlanID: planID, PaymentSessionID: sessionID, PaymentType: method, OrderType: payment.OrderTypeSubscription}
	plan, err := s.validateSubOrder(ctx, req)
	if err != nil {
		return nil, err
	}
	cfg, err := s.configService.GetPaymentConfig(ctx)
	if err != nil {
		return nil, err
	}
	currency, err := s.configService.ValidateMethodCurrencyConsistency(ctx, method)
	if err != nil {
		return nil, err
	}
	quote := &SubscriptionCheckout{}
	if sessionID > 0 {
		session, err := s.entClient.PaymentSession.Get(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		quote.TotalCents = int64(math.Round(session.TargetAmount * 100))
		if session.Status != "PENDING" {
			return quote, nil
		}
		orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.PaymentSessionIDEQ(sessionID), paymentorder.PaidAtIsNil(), paymentorder.ProviderClosedEQ(false)).Order(dbent.Desc(paymentorder.FieldID)).All(ctx)
		if err != nil {
			return nil, err
		}
		if len(orders) > 0 {
			o := orders[0]
			quote.PendingOrder = &SubscriptionPendingPayment{ID: o.ID, PaymentType: o.PaymentType, PayAmount: o.PayAmount, Currency: PaymentOrderCurrency(o), ExpiresAt: o.ExpiresAt, OutTradeNo: o.OutTradeNo, Checking: o.Status != OrderStatusPending || time.Now().After(o.ExpiresAt) || psStringValue(o.PayURL) == "" && psStringValue(o.QrCode) == ""}
			if !quote.PendingOrder.Checking {
				quote.PendingOrder.PayURL = psStringValue(o.PayURL)
				quote.PendingOrder.QRCode = psStringValue(o.QrCode)
				quote.PendingOrder.QRCodeImg = psStringValue(o.QrCodeImg)
			}
			return quote, nil
		}
	}
	installment, err := s.prepareSubscriptionInstallment(ctx, req, plan, cfg, currency)
	if err != nil {
		return nil, err
	}
	quote.TotalCents = int64(math.Round(installment.Target * 100))
	quote.NextAmount = installment.Amount
	return quote, nil
}
func (s *PaymentService) ReconcileSubscriptionSession(ctx context.Context, userID, sessionID int64) error {
	session, err := s.entClient.PaymentSession.Get(ctx, sessionID)
	if err != nil || session.UserID != userID {
		return infraerrors.NotFound("NOT_FOUND", "bill not found")
	}
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.PaymentSessionIDEQ(sessionID), paymentorder.PaidAtIsNil(), paymentorder.ProviderClosedEQ(false)).All(ctx)
	if err != nil {
		return err
	}
	for _, order := range orders {
		s.reconcilePaid(ctx, order)
	}
	return nil
}
