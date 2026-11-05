package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentsession"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const paymentSessionCancelling = "CANCELLING"
const paymentSessionCancelled = "CANCELLED"
const paymentProviderCreateInProgress = "PROVIDER_CREATE_IN_PROGRESS"
const paymentSessionReviewNeeded = "PAYMENT_AFTER_SESSION_CANCELLED_REVIEW_NEEDED"

func unpaidLegacySession(session *dbent.PaymentSession, orders []*dbent.PaymentOrder) error {
	if session.PaidAmount != 0 || session.AllocationOrderID != nil || session.AllocatedAt != nil || session.Status == "PAID" || session.Status == "FULFILLED" || session.Status == "REFUNDED" {
		return infraerrors.Conflict("PAYMENT_IN_PROGRESS", "this bill already has a confirmed or processing payment")
	}
	// Check every sibling before making ANY provider cancellation call.
	for _, o := range orders {
		if o.PaidAt != nil || o.Status == OrderStatusPaid || o.Status == OrderStatusRecharging || o.Status == OrderStatusCompleted || psIsRefundStatus(o.Status) {
			return infraerrors.Conflict("PAYMENT_IN_PROGRESS", "this bill already has a confirmed or processing payment")
		}
		if psStringValue(o.FailedReason) == paymentProviderCreateInProgress && time.Since(o.CreatedAt) < 45*time.Second {
			return infraerrors.Conflict("PAYMENT_CHECKING", "a gateway order is still being created")
		}
	}
	return nil
}

// CancelSubscriptionSession closes only wholly unpaid legacy bills. CANCELLING
// fences new installments while bounded provider work runs outside transactions.
// On uncertainty the fence stays in place, is visible, and can safely be retried.
func (s *PaymentService) CancelSubscriptionSession(ctx context.Context, userID, sessionID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockPaymentUser(ctx, tx.Client(), userID); err != nil {
		return err
	}
	session, err := tx.PaymentSession.Query().Where(paymentsession.IDEQ(sessionID), paymentsession.UserIDEQ(userID)).Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return infraerrors.NotFound("NOT_FOUND", "bill not found")
		}
		return err
	}
	if session.Status == paymentSessionCancelled {
		return nil
	}
	orders, err := tx.PaymentOrder.Query().Where(paymentorder.PaymentSessionIDEQ(sessionID)).Order(paymentorder.ByID()).All(ctx)
	if err != nil {
		return err
	}
	if err := unpaidLegacySession(session, orders); err != nil {
		return err
	}
	if session.Status != "PENDING" && session.Status != "ALLOCATION_FAILED" && session.Status != paymentSessionCancelling {
		return infraerrors.Conflict("PAYMENT_SESSION_CLOSED", "bill is closed")
	}
	if len(orders) > 20 {
		return infraerrors.Conflict("PAYMENT_CHECKING", "too many legacy orders; operator review required")
	}
	if _, err := tx.PaymentSession.UpdateOneID(sessionID).SetStatus(paymentSessionCancelling).Save(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	type closure struct {
		order  *dbent.PaymentOrder
		prov   payment.Provider
		ref    string
		closed bool
		// upstream is true only when the gateway itself closed the order.
		upstream bool
		// terminal: the query already reported a closed/failed trade.
		terminal bool
	}
	proofs := make([]closure, 0, len(orders))
	// Query ALL siblings first. An upstream paid/processing/unknown result must
	// prevent cancellation of any other order, even if it sorts earlier.
	for _, o := range orders {
		item := closure{order: o, closed: o.ProviderClosed, upstream: o.ProviderClosed}
		if !item.closed {
			callCtx, stop := context.WithTimeout(ctx, 8*time.Second)
			prov, err := s.getOrderProvider(callCtx, o)
			if err != nil {
				stop()
				return paymentClosureUnknown(sessionID, o.ID, "provider", err)
			}
			ref := paymentOrderQueryReference(o, prov)
			if ref == "" {
				stop()
				return paymentClosureUnknown(sessionID, o.ID, "reference", nil)
			}
			resp, err := prov.QueryOrder(callCtx, ref)
			stop()
			if err != nil || resp == nil {
				return paymentClosureUnknown(sessionID, o.ID, "query", err)
			}
			if resp.Status == payment.ProviderStatusPaid {
				if err := s.HandlePaymentNotification(ctx, &payment.PaymentNotification{OrderID: o.OutTradeNo, TradeNo: resp.TradeNo, Amount: resp.Amount, Status: payment.NotificationStatusSuccess, Metadata: resp.Metadata}, prov.ProviderKey()); err != nil {
					return err
				}
				return infraerrors.Conflict("PAYMENT_IN_PROGRESS", "the gateway reports a paid bill")
			}
			if providerPaymentInProgress(resp) {
				return infraerrors.Conflict("PAYMENT_IN_PROGRESS", "the gateway is processing a payment")
			}
			if resp.Status != payment.ProviderStatusPending && resp.Status != payment.ProviderStatusFailed {
				return paymentClosureUnknown(sessionID, o.ID, "status:"+resp.Status, nil)
			}
			item.prov, item.ref = prov, ref
			item.terminal = resp.Status == payment.ProviderStatusFailed
		}
		proofs = append(proofs, item)
	}
	for i := range proofs {
		item := &proofs[i]
		if item.closed {
			continue
		}
		cp, ok := item.prov.(payment.CancelableProvider)
		if !ok {
			// Aggregators such as EasyPay have no close API. The query above
			// proved this order unpaid, so close it locally like cancelCore does;
			// a later payment is still kept for review by recordCancelledSessionPayment.
			slog.Info("payment session order closed locally; provider has no close API", "sessionID", sessionID, "orderID", item.order.ID, "provider", item.prov.ProviderKey())
			item.closed = true
			continue
		}
		callCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		err := cp.CancelPayment(callCtx, item.ref)
		stop()
		if err != nil {
			if !item.terminal {
				return paymentClosureUnknown(sessionID, item.order.ID, "cancel", err)
			}
			// Gateways reject closing an already closed trade (e.g. Alipay
			// TRADE_CLOSED); the query already proved it terminal and unpaid.
			slog.Info("payment session order already closed upstream", "sessionID", sessionID, "orderID", item.order.ID, "error", err)
		}
		item.closed, item.upstream = true, true
	}

	final, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = final.Rollback() }()
	if err := lockPaymentUser(ctx, final.Client(), userID); err != nil {
		return err
	}
	session, err = final.PaymentSession.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	fresh, err := final.PaymentOrder.Query().Where(paymentorder.PaymentSessionIDEQ(sessionID)).All(ctx)
	if err != nil {
		return err
	}
	if err := unpaidLegacySession(session, fresh); err != nil {
		return err
	}
	if session.Status == paymentSessionCancelled {
		return nil
	}
	if session.Status != paymentSessionCancelling || len(fresh) != len(proofs) {
		return paymentClosureUnknown(sessionID, 0, "recheck", nil)
	}
	for _, o := range fresh {
		proven, upstream := false, false
		for _, proof := range proofs {
			if proof.order.ID == o.ID && proof.closed {
				proven, upstream = true, proof.upstream
				break
			}
		}
		if !proven {
			return paymentClosureUnknown(sessionID, o.ID, "recheck", nil)
		}
		if _, err := final.PaymentOrder.UpdateOneID(o.ID).SetProviderClosed(upstream).SetStatus(OrderStatusCancelled).Save(ctx); err != nil {
			return err
		}
	}
	if _, err := final.PaymentSession.UpdateOneID(sessionID).SetStatus(paymentSessionCancelled).Save(ctx); err != nil {
		return err
	}
	if err := writePaymentSessionAudit(ctx, final.Client(), sessionID, "PAYMENT_SESSION_CANCELLED", fmt.Sprintf("user:%d", userID), map[string]any{"session_id": sessionID}); err != nil {
		return err
	}
	return final.Commit()
}

func paymentClosureUnknown(sessionID, orderID int64, stage string, cause error) error {
	slog.Warn("payment session cancellation not proven", "sessionID", sessionID, "orderID", orderID, "stage", stage, "error", cause)
	return infraerrors.Conflict("PAYMENT_CHECKING", "the gateway has not confirmed closure; this bill remains recoverable")
}

func providerPaymentInProgress(resp *payment.QueryOrderResponse) bool {
	for _, value := range []string{resp.Status, resp.Metadata["status"], resp.Metadata["trade_state"]} {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "PROCESSING", "USERPAYING", "REQUIRES_CAPTURE", "PENDING_REVIEW":
			return true
		}
	}
	return false
}

// CleanupUnpaidLegacySessions is an operator entry point; dry-run is the default
// (apply=false). It never runs on GET, never deletes rows, and applies the exact
// same provider-proof and final-recheck rules as interactive cancellation.
func (s *PaymentService) CleanupUnpaidLegacySessions(ctx context.Context, apply bool, limit int) ([]LegacySessionCleanupResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	sessions, err := s.entClient.PaymentSession.Query().Where(paymentsession.StatusIn("PENDING", "ALLOCATION_FAILED", paymentSessionCancelling)).Order(paymentsession.ByID()).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]LegacySessionCleanupResult, 0, len(sessions))
	for _, session := range sessions {
		orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.PaymentSessionIDEQ(session.ID)).All(ctx)
		if err != nil {
			return results, err
		}
		result := LegacySessionCleanupResult{SessionID: session.ID, Status: "candidate"}
		if err := unpaidLegacySession(session, orders); err != nil {
			result.Status = "retained"
			result.Detail = err.Error()
		} else if apply {
			if err := s.CancelSubscriptionSession(ctx, session.UserID, session.ID); err != nil {
				result.Status = "retained"
				result.Detail = err.Error()
			} else {
				result.Status = "cancelled"
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func recordCancelledSessionPayment(ctx context.Context, client *dbent.Client, session *dbent.PaymentSession, o *dbent.PaymentOrder, tradeNo string, paid float64, pk string) error {
	// Persist the money, but deliberately leave the bill CANCELLED. Failed +
	// paid_at keeps this visible in existing order/admin review and refund flows;
	// every fulfillment retry also checks the terminal session before allocating.
	if o.PaidAt == nil {
		if _, err := client.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusFailed).
			SetPaidAt(time.Now()).SetPayAmount(paid).SetPaymentTradeNo(tradeNo).
			SetFailedAt(time.Now()).SetFailedReason(paymentSessionReviewNeeded).Save(ctx); err != nil {
			return err
		}
	}
	orders, err := client.PaymentOrder.Query().Where(paymentorder.PaymentSessionIDEQ(session.ID), paymentorder.PaidAtNotNil()).All(ctx)
	if err != nil {
		return err
	}
	total := 0.0
	for _, order := range orders {
		total += order.PayAmount
	}
	if _, err := client.PaymentSession.UpdateOneID(session.ID).SetPaidAmount(total).SetAllocationError(paymentSessionReviewNeeded).Save(ctx); err != nil {
		return err
	}
	return writePaymentSessionAudit(ctx, client, o.ID, "PAYMENT_AFTER_SESSION_CANCELLED", pk, map[string]any{"session_id": session.ID, "trade_no": tradeNo, "paid_amount": paid, "review_needed": true})
}

func writePaymentSessionAudit(ctx context.Context, client *dbent.Client, id int64, action, operator string, detail map[string]any) error {
	// Session-only events use a distinct namespace, never an unrelated order ID.
	key := fmt.Sprintf("%d", id)
	if action == "PAYMENT_SESSION_CANCELLED" {
		key = fmt.Sprintf("session:%d", id)
	}
	exists, err := client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(key), paymentauditlog.ActionEQ(action)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = client.PaymentAuditLog.Create().SetOrderID(key).SetAction(action).SetOperator(operator).SetDetail(string(encoded)).Save(ctx)
	return err
}

type LegacySessionCleanupResult struct {
	SessionID int64  `json:"session_id"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
}
