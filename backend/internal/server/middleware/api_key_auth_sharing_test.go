//go:build unit

package middleware

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIKeyAuthSharedSubscriptionReachesHandlerWithContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lim := 10.0
	source := &service.Group{ID: 11, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	keyGroup := &service.Group{ID: 20, SubscriptionType: service.SubscriptionTypeStandard, SubscriptionSourceGroupIDs: []int64{11}, Status: service.StatusActive}
	user := &service.User{ID: 7, Status: service.StatusActive, Balance: 10}
	key := &service.APIKey{ID: 2, Key: "share-key", Status: service.StatusActive, User: user, Group: keyGroup}
	keyGroupID := keyGroup.ID
	key.GroupID = &keyGroupID
	now := time.Now()
	sub := &service.UserSubscription{ID: 3, UserID: 7, GroupID: 11, Group: source, Status: service.SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	subRepo := &stubUserSubscriptionRepo{getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
		if groupID != 11 {
			return nil, service.ErrSubscriptionNotFound
		}
		return sub, nil
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiSvc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	subSvc := service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
	defer subSvc.Stop()
	r := gin.New()
	var seen *service.UserSubscription
	r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiSvc, subSvc, cfg)))
	r.GET("/t", func(c *gin.Context) {
		v, ok := c.Get(string(ContextKeySubscription))
		require.True(t, ok)
		seen = v.(*service.UserSubscription)
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("x-api-key", key.Key)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, int64(3), seen.ID)
}

// overflowFixture builds a subscription-type key group whose subscription has used up its daily limit.
func overflowFixtureFlags(t *testing.T, adminAllowed, overflow bool, balance float64) (*gin.Engine, *service.APIKey, func() (*service.UserSubscription, bool)) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	lim := 1.0
	keyGroup := &service.Group{ID: 17, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &lim, Status: service.StatusActive}
	user := &service.User{ID: 4, Status: service.StatusActive, Balance: balance, BalanceOverflowAdminAllowed: adminAllowed, BalanceOverflowEnabled: overflow}
	key := &service.APIKey{ID: 5, Key: "overflow-key", Status: service.StatusActive, User: user, Group: keyGroup}
	gid := keyGroup.ID
	key.GroupID = &gid
	now := time.Now()
	sub := &service.UserSubscription{ID: 27, UserID: 4, GroupID: 17, Group: keyGroup, Status: service.SubscriptionStatusActive, ExpiresAt: now.Add(24 * time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, DailyUsageUSD: 2}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	subRepo := &stubUserSubscriptionRepo{getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
		if groupID != 17 {
			return nil, service.ErrSubscriptionNotFound
		}
		cp := *sub
		return &cp, nil
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiSvc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	subSvc := service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
	t.Cleanup(subSvc.Stop)
	var seen *service.UserSubscription
	var seenOK bool
	r := gin.New()
	r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiSvc, subSvc, cfg)))
	handler := func(c *gin.Context) {
		v, ok := c.Get(string(ContextKeySubscription))
		seenOK = ok
		if ok {
			seen = v.(*service.UserSubscription)
		}
		c.Status(http.StatusNoContent)
	}
	r.GET("/t", handler)
	r.GET("/v1/usage", handler)
	return r, key, func() (*service.UserSubscription, bool) { return seen, seenOK }
}

func serveWithKey(r *gin.Engine, key *service.APIKey, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("x-api-key", key.Key)
	r.ServeHTTP(w, req)
	return w
}

func TestAPIKeyAuthOverflowToBalanceProceedsWithoutSubscription(t *testing.T) {
	r, key, seen := overflowFixture(t, true, 10)
	w := serveWithKey(r, key, "/t")
	require.Equal(t, http.StatusNoContent, w.Code)
	_, ok := seen()
	require.False(t, ok, "overflow request must be balance-billed (no subscription in context)")
}

// 额度用完且余额也不足时，仍返回订阅限额错误（比「余额不足」更能说明原因）。
func TestAPIKeyAuthOverflowWithoutBalanceKeepsUsageLimit(t *testing.T) {
	r, key, _ := overflowFixture(t, true, 0)
	w := serveWithKey(r, key, "/t")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "USAGE_LIMIT_EXCEEDED")
}

func TestAPIKeyAuthOverflowOffKeepsUsageLimit(t *testing.T) {
	r, key, _ := overflowFixture(t, false, 10)
	w := serveWithKey(r, key, "/t")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "USAGE_LIMIT_EXCEEDED")
}

func TestAPIKeyAuthUsageEndpointStillShowsExhaustedSubscription(t *testing.T) {
	r, key, seen := overflowFixture(t, true, 10)
	w := serveWithKey(r, key, "/v1/usage")
	require.Equal(t, http.StatusNoContent, w.Code)
	sub, ok := seen()
	require.True(t, ok, "/v1/usage must keep reading the subscription for display")
	require.Equal(t, int64(27), sub.ID)
}

func overflowFixture(t *testing.T, overflow bool, balance float64) (*gin.Engine, *service.APIKey, func() (*service.UserSubscription, bool)) {
	return overflowFixtureFlags(t, true, overflow, balance)
}

// The admin permission overrides the user's own switch.
func TestAPIKeyAuthAdminDisallowOverridesUserOverflow(t *testing.T) {
	r, key, _ := overflowFixtureFlags(t, false, true, 10)
	w := serveWithKey(r, key, "/t")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "USAGE_LIMIT_EXCEEDED")
}

// A standard group set to "any subscription" bills the user's earliest-expiring active subscription.
func TestAPIKeyAuthSourceAnyUsesEarliestExpiringSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lim := 10.0
	planA := &service.Group{ID: 17, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	planB := &service.Group{ID: 18, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &lim}
	keyGroup := &service.Group{ID: 23, SubscriptionType: service.SubscriptionTypeStandard, SubscriptionSourceAny: true, Status: service.StatusActive}
	user := &service.User{ID: 4, Status: service.StatusActive, Balance: 10, BalanceOverflowAdminAllowed: true, BalanceOverflowEnabled: true}
	key := &service.APIKey{ID: 6, Key: "any-key", Status: service.StatusActive, User: user, Group: keyGroup}
	gid := keyGroup.ID
	key.GroupID = &gid
	now := time.Now()
	later := &service.UserSubscription{ID: 31, UserID: 4, GroupID: 17, Group: planA, Status: service.SubscriptionStatusActive, ExpiresAt: now.Add(30 * 24 * time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	sooner := &service.UserSubscription{ID: 32, UserID: 4, GroupID: 18, Group: planB, Status: service.SubscriptionStatusActive, ExpiresAt: now.Add(5 * 24 * time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	subRepo := &stubUserSubscriptionRepo{
		listActive: func(context.Context, int64) ([]service.UserSubscription, error) {
			return []service.UserSubscription{*later, *sooner}, nil
		},
		getActive: func(_ context.Context, _ int64, groupID int64) (*service.UserSubscription, error) {
			switch groupID {
			case 17:
				cp := *later
				return &cp, nil
			case 18:
				cp := *sooner
				return &cp, nil
			}
			return nil, service.ErrSubscriptionNotFound
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiSvc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	subSvc := service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
	defer subSvc.Stop()
	var seen *service.UserSubscription
	r := gin.New()
	r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiSvc, subSvc, cfg)))
	r.GET("/t", func(c *gin.Context) {
		v, ok := c.Get(string(ContextKeySubscription))
		require.True(t, ok)
		seen = v.(*service.UserSubscription)
		c.Status(http.StatusNoContent)
	})
	w := serveWithKey(r, key, "/t")
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, int64(32), seen.ID, "earliest-expiring subscription pays first")
}
