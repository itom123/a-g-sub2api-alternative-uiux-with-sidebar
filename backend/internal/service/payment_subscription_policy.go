package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	entuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// This lock order (user, then session/order) is shared by checkout, cancellation,
// payment confirmation and subscription assignment. Never call providers in a tx.
func lockPaymentUser(ctx context.Context, client *dbent.Client, userID int64) error {
	q := client.User.Query().Where(entuser.IDEQ(userID))
	if paymentAuditDialect(client) == dialect.Postgres {
		q.ForUpdate()
	}
	if _, err := q.Only(ctx); err != nil {
		return fmt.Errorf("lock payment user: %w", err)
	}
	return nil
}

// Payment-only policy: do not change admin/redeem same-group renewal semantics.
func (s *PaymentService) checkActiveSubscriptionPlatform(ctx context.Context, userID int64, platform string) error {
	client := s.entClient
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	if client == nil {
		return fmt.Errorf("subscription policy database is unavailable")
	}
	exists, err := client.UserSubscription.Query().Where(
		usersubscription.UserIDEQ(userID),
		usersubscription.StatusEQ(SubscriptionStatusActive),
		usersubscription.ExpiresAtGT(time.Now()),
		usersubscription.DeletedAtIsNil(),
		usersubscription.HasGroupWith(group.PlatformEQ(platform), group.DeletedAtIsNil()),
	).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check active platform subscription: %w", err)
	}
	if exists {
		return ErrSubscriptionPlatformAlreadyActive
	}
	return nil
}

// Shared by quote and create. A fixed CNY price is already the final gateway
// amount; neither fees nor FX nor legacy installment schedules apply to it.
func subscriptionOneTimePayAmount(plan *dbent.SubscriptionPlan, cfg *PaymentConfig, currency string, expected *int64) (string, float64, error) {
	var text string
	var amount float64
	var err error
	if plan.FixedPriceCny > 0 {
		if currency != payment.DefaultPaymentCurrency {
			return "", 0, infraerrors.BadRequest("PAYMENT_SESSION_CURRENCY", "this subscription must be paid in CNY")
		}
		amount = plan.FixedPriceCny
		if err = validateCreateOrderAmountCurrency(amount, currency); err != nil {
			return "", 0, err
		}
		text = strconv.FormatFloat(amount, 'f', 2, 64)
	} else {
		text, amount, err = calculateCreateOrderPayAmountForOrderType(plan.Price, cfg.RechargeFeeRate, currency, payment.OrderTypeSubscription, cfg.SubscriptionUSDToCNYRate)
		if err != nil {
			return "", 0, err
		}
	}
	if !isValidProviderAmount(amount) {
		return "", 0, infraerrors.BadRequest("INVALID_AMOUNT", "subscription amount must be positive")
	}
	if expected != nil && *expected != int64(math.Round(amount*100)) {
		return "", 0, infraerrors.Conflict("QUOTE_CHANGED", "the subscription total changed; review the updated quote before paying")
	}
	return text, amount, nil
}
