package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"time"
)

// ErrSubscriptionMaintenanceFailed identifies failures while synchronously
// maintaining subscription usage windows before authorization.
var ErrSubscriptionMaintenanceFailed = errors.New("subscription maintenance failed")

// SubscriptionBillingGroup returns the group whose subscription quota and
// usage cache should be used, or nil when the request is balance-billed.
func SubscriptionBillingGroup(keyGroup *Group, sub *UserSubscription) *Group {
	if sub == nil || keyGroup == nil {
		return nil
	}
	if keyGroup.IsSubscriptionType() && sub.GroupID == keyGroup.ID {
		return keyGroup
	}
	if sub.Group != nil && sub.Group.IsSubscriptionType() && sub.GroupID != keyGroup.ID && !keyGroup.IsSubscriptionType() &&
		(keyGroup.SubscriptionSourceAny || keyGroup.AllowsSubscriptionSource(sub.GroupID)) {
		return sub.Group
	}
	return nil
}

func isSubscriptionLimitError(err error) bool {
	return errors.Is(err, ErrDailyLimitExceeded) || errors.Is(err, ErrWeeklyLimitExceeded) || errors.Is(err, ErrMonthlyLimitExceeded)
}

// ResolveBillingSubscription resolves and validates the subscription that will
// pay for a request. A nil subscription means the request is balance-billed.
//
// Subscribers are charged against their allowance first. Once it is used up the
// balance pays, but only when the user's balance overflow is allowed (admin
// permission AND the user's own switch) and the balance is above the auth
// threshold; otherwise the limit error stands.
func (s *SubscriptionService) ResolveBillingSubscription(ctx context.Context, user *User, keyGroup *Group, balanceOK bool) (*UserSubscription, error) {
	if s == nil || keyGroup == nil || user == nil {
		return nil, nil
	}
	overflow := balanceOK && user.BalanceOverflowAllowed()
	if keyGroup.IsSubscriptionType() {
		sub, err := s.GetActiveSubscription(ctx, user.ID, keyGroup.ID)
		if err != nil {
			// 与改造前一致：任何查询失败都按「无有效订阅」拒绝，不向客户端暴露内部错误。
			if !errors.Is(err, ErrSubscriptionNotFound) {
				slog.Warn("subscription lookup failed", "user_id", user.ID, "group_id", keyGroup.ID, "error", err)
			}
			return nil, ErrSubscriptionNotFound
		}
		validated, err := s.validateBillingSubscription(ctx, sub, keyGroup)
		if err != nil && overflow && isSubscriptionLimitError(err) {
			return nil, nil
		}
		return validated, err
	}

	var sourceIDs []int64
	switch {
	case keyGroup.SubscriptionSourceAny:
		ids, err := s.activeSubscriptionGroupIDsByExpiry(ctx, user.ID)
		if err != nil {
			slog.Warn("subscription source list failed", "user_id", user.ID, "group_id", keyGroup.ID, "error", err)
			return nil, errors.Join(ErrSubscriptionMaintenanceFailed, err)
		}
		sourceIDs = ids
	case len(keyGroup.SubscriptionSourceGroupIDs) > 0:
		sourceIDs = keyGroup.SubscriptionSourceGroupIDs
	default:
		return nil, nil
	}

	// 共享订阅：按顺序取第一个仍有额度的订阅。没有可用订阅时按余额计费；有订阅但额度都已用完时，
	// 只有允许使用余额才改扣余额，否则返回限额错误。查询或窗口维护失败时不能静默改扣余额，按维护失败拒绝。
	var exhausted error
	for _, sourceID := range sourceIDs {
		sub, err := s.GetActiveSubscription(ctx, user.ID, sourceID)
		if err != nil {
			if errors.Is(err, ErrSubscriptionNotFound) {
				continue
			}
			slog.Warn("subscription source lookup failed", "user_id", user.ID, "group_id", sourceID, "error", err)
			return nil, errors.Join(ErrSubscriptionMaintenanceFailed, err)
		}
		if sub == nil || sub.DeletedAt != nil || sub.Group == nil || !sub.Group.IsSubscriptionType() || sub.GroupID != sourceID {
			continue
		}
		validated, err := s.validateBillingSubscription(ctx, sub, sub.Group)
		if err != nil {
			if errors.Is(err, ErrSubscriptionMaintenanceFailed) {
				slog.Warn("subscription source maintenance failed", "user_id", user.ID, "group_id", sourceID, "error", err)
				return nil, err
			}
			if isSubscriptionLimitError(err) && exhausted == nil {
				exhausted = err
			}
			// 限额用完、过期或暂停：换下一个来源
			slog.Debug("subscription source unavailable", "user_id", user.ID, "group_id", sourceID, "error", err)
			continue
		}
		if validated != nil {
			return validated, nil
		}
	}
	if exhausted != nil && !overflow {
		return nil, exhausted
	}
	return nil, nil
}

func (s *SubscriptionService) validateBillingSubscription(ctx context.Context, sub *UserSubscription, group *Group) (*UserSubscription, error) {
	needsMaintenance, err := s.ValidateAndCheckLimits(sub, group)
	if needsMaintenance {
		refreshed, maintenanceErr := s.EnsureWindowMaintenance(ctx, sub)
		if maintenanceErr != nil {
			return nil, errors.Join(ErrSubscriptionMaintenanceFailed, maintenanceErr)
		}
		// 计费模式判定依赖 sub.Group（共享订阅），回读结果缺少分组时沿用原分组。
		if refreshed != nil && refreshed.Group == nil {
			refreshed.Group = sub.Group
		}
		sub = refreshed
		_, err = s.ValidateAndCheckLimits(sub, group)
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// subscriptionGroupRef is the cached part of a user's active subscription list:
// only which subscription groups the user holds and when each expires. Usage and
// limits are always read through GetActiveSubscription, so a stale list can at
// worst miss a just-assigned subscription for one TTL, never bill stale usage.
type subscriptionGroupRef struct {
	GroupID   int64
	ExpiresAt time.Time
	ID        int64
}

func subListCacheKey(userID int64) string {
	return "subs:" + strconv.FormatInt(userID, 10)
}

// activeSubscriptionGroupIDsByExpiry returns the subscription-type groups of the user's
// active subscriptions, earliest expiry first (then subscription ID), for "any subscription" groups.
func (s *SubscriptionService) activeSubscriptionGroupIDsByExpiry(ctx context.Context, userID int64) ([]int64, error) {
	key := subListCacheKey(userID)
	var refs []subscriptionGroupRef
	if s.subCacheL1 != nil {
		if v, ok := s.subCacheL1.Get(key); ok {
			if cached, ok := v.([]subscriptionGroupRef); ok {
				refs = cached
			}
		}
	}
	if refs == nil {
		value, err, _ := s.subCacheGroup.Do(key, func() (any, error) {
			subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
			if err != nil {
				return nil, err
			}
			out := make([]subscriptionGroupRef, 0, len(subs))
			for _, sub := range subs {
				if sub.Group == nil || !sub.Group.IsSubscriptionType() {
					continue
				}
				out = append(out, subscriptionGroupRef{GroupID: sub.GroupID, ExpiresAt: sub.ExpiresAt, ID: sub.ID})
			}
			sort.SliceStable(out, func(i, j int) bool {
				if !out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
					return out[i].ExpiresAt.Before(out[j].ExpiresAt)
				}
				return out[i].ID < out[j].ID
			})
			if s.subCacheL1 != nil {
				_ = s.subCacheL1.SetWithTTL(key, out, 1, s.jitteredTTL(s.subCacheTTL))
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		refs, _ = value.([]subscriptionGroupRef)
	}
	ids := make([]int64, 0, len(refs))
	seen := make(map[int64]struct{}, len(refs))
	for _, ref := range refs {
		if _, ok := seen[ref.GroupID]; ok {
			continue
		}
		seen[ref.GroupID] = struct{}{}
		ids = append(ids, ref.GroupID)
	}
	return ids, nil
}
