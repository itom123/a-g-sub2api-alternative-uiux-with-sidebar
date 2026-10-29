package service

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"strconv"
	"strings"
)

// Subscription purchases are opt-in. Only administrators and accounts explicitly
// selected in the payment settings can see plans or create subscription orders.
func (s *PaymentService) SubscriptionAccessAllowed(ctx context.Context, userID int64) bool {
	if s.userRepo == nil {
		return false
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	if s.configService == nil {
		return false
	}
	raw, err := s.configService.GetSubscriptionSidebarAllowlist(ctx)
	if err != nil {
		return false
	}
	wantID := strconv.FormatInt(userID, 10)
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
		entry := strings.TrimSpace(item)
		if entry == wantID || strings.EqualFold(entry, user.Email) {
			return true
		}
	}
	return false
}

func (s *PaymentService) subscriptionPlanAllowed(ctx context.Context, userID int64, plan *dbent.SubscriptionPlan) bool {
	if len(plan.EligibleUserIds) == 0 {
		return true
	}
	if s.userRepo == nil {
		return false
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	for _, id := range plan.EligibleUserIds {
		if id == userID {
			return true
		}
	}
	return false
}
