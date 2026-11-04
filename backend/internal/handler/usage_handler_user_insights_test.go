package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type snapshotInsightsRepo struct {
	userUsageRepoCapture
	called      bool
	query       usagestats.UserInsightsQuery
	err         error
	hasDeadline bool
}

func (r *snapshotInsightsRepo) GetUserKeyActivity(ctx context.Context, q usagestats.UserInsightsQuery) (*usagestats.UserKeyActivity, error) {
	r.called = true
	r.query = q
	_, r.hasDeadline = ctx.Deadline()
	if r.err != nil {
		return nil, r.err
	}
	return &usagestats.UserKeyActivity{AsOf: q.AsOf, RangeStart: q.RangeStart, RangeEndExclusive: q.RangeEndExclusive, Coverage: "retained_logs", Keys: []usagestats.UserKeyInsight{}, ActiveWindows: []usagestats.UserActiveWindow{}}, nil
}
func snapshotInsightsRouter(repo service.UsageLogRepository, authenticated bool, keys *dailyUsageAPIKeyRepoStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	var apiKeySvc *service.APIKeyService
	if keys != nil {
		apiKeySvc = service.NewAPIKeyService(keys, nil, nil, nil, nil, nil, nil)
	}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), apiKeySvc, nil, nil)
	r := gin.New()
	if authenticated {
		r.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42}) })
	}
	r.GET("/snapshot", h.DashboardSnapshotV2)
	return r
}
func snapshotInsightsRequest(r *gin.Engine, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot?"+query, nil))
	return rec
}
func TestSnapshotKeyActivityOptInAndScopedFilters(t *testing.T) {
	repo := &snapshotInsightsRepo{}
	router := snapshotInsightsRouter(repo, true, nil)
	rec := snapshotInsightsRequest(router, "include_trend=false&include_model_stats=false")
	require.Equal(t, http.StatusOK, rec.Code)
	require.False(t, repo.called)
	require.NotContains(t, rec.Body.String(), "key_activity")
	rec = snapshotInsightsRequest(router, "include_key_activity=true&include_trend=false&include_model_stats=false&timezone=America%2FNew_York&group_id=9&model=requested&billing_type=1&billing_mode=token&request_type=ws_v2&native_compaction_v2=true&user_id=99")
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.called)
	require.True(t, repo.hasDeadline)
	require.Equal(t, int64(42), repo.query.Filters.UserID)
	require.Equal(t, int64(9), repo.query.Filters.GroupID)
	require.Equal(t, "requested", repo.query.Filters.Model)
	require.Equal(t, "token", repo.query.Filters.BillingMode)
	require.Equal(t, int8(1), *repo.query.Filters.BillingType)
	require.Equal(t, int16(service.RequestTypeWSV2), *repo.query.Filters.RequestType)
	require.True(t, *repo.query.Filters.NativeCompactionV2)
	require.Nil(t, repo.query.Filters.StartTime)
	require.Equal(t, repo.query.AsOf, repo.query.RangeEndExclusive)
	require.Contains(t, rec.Body.String(), `"key_activity_status":"available"`)
	require.NotContains(t, rec.Body.String(), "account_cost")
	require.NotContains(t, rec.Body.String(), `"key":`)
	var envelope struct {
		Data struct {
			KeyActivity usagestats.UserKeyActivity `json:"key_activity"`
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.True(t, envelope.Data.KeyActivity.AsOf.Equal(repo.query.AsOf))
}
func TestSnapshotKeyActivityPartialFailurePreservesCharts(t *testing.T) {
	for _, failure := range []error{errors.New("secret database host and SQL"), context.Canceled, context.DeadlineExceeded} {
		repo := &snapshotInsightsRepo{err: failure}
		rec := snapshotInsightsRequest(snapshotInsightsRouter(repo, true, nil), "include_key_activity=true")
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), `"trend":[]`)
		require.Contains(t, rec.Body.String(), `"models":[]`)
		require.Contains(t, rec.Body.String(), `"key_activity":null`)
		require.Contains(t, rec.Body.String(), `"key_activity_status":"unavailable"`)
		require.Contains(t, rec.Body.String(), `"key_activity_error":"KEY_ACTIVITY_UNAVAILABLE"`)
		require.NotContains(t, rec.Body.String(), failure.Error())
	}
	rec := snapshotInsightsRequest(snapshotInsightsRouter(&userUsageRepoCapture{}, true, nil), "include_key_activity=true")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"key_activity_error":"KEY_ACTIVITY_UNAVAILABLE"`)
}
func TestSnapshotKeyActivityInputValidation(t *testing.T) {
	for _, query := range []string{
		"include_key_activity=bad", "include_key_activity=true&timezone=Bad%2FZone", "include_key_activity=true&timezone=Local",
		"include_key_activity=true&start_date=2026-02-02&end_date=2026-02-01",
		"include_key_activity=true&start_date=2026-01-01&end_date=2026-01-31",
		"include_key_activity=true&start_date=2099-01-01&end_date=2099-01-02",
		"include_key_activity=true&start_date=2026-02-30",
	} {
		t.Run(query, func(t *testing.T) {
			repo := &snapshotInsightsRepo{}
			rec := snapshotInsightsRequest(snapshotInsightsRouter(repo, true, nil), query)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.False(t, repo.called)
		})
	}
	// Optional section validation must not change legacy timezone fallback.
	rec := snapshotInsightsRequest(snapshotInsightsRouter(&userUsageRepoCapture{}, true, nil), "timezone=Bad%2FZone")
	require.Equal(t, http.StatusOK, rec.Code)
}
func TestSnapshotKeyActivityAuthorization(t *testing.T) {
	repo := &snapshotInsightsRepo{}
	rec := snapshotInsightsRequest(snapshotInsightsRouter(repo, false, nil), "include_key_activity=true")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, repo.called)
	keys := &dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{7: {ID: 7, UserID: 99}}}
	rec = snapshotInsightsRequest(snapshotInsightsRouter(repo, true, keys), "include_key_activity=true&api_key_id=7")
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.False(t, repo.called)
	keys.keys[7].UserID = 42
	rec = snapshotInsightsRequest(snapshotInsightsRouter(repo, true, keys), "include_key_activity=true&api_key_id=7&include_trend=false&include_model_stats=false")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), repo.query.Filters.APIKeyID)
}
func TestSnapshotKeyActivityHistoricalRangeIsSeparateFromWindows(t *testing.T) {
	repo := &snapshotInsightsRepo{}
	rec := snapshotInsightsRequest(snapshotInsightsRouter(repo, true, nil), "include_key_activity=true&include_trend=false&include_model_stats=false&timezone=America%2FNew_York&start_date=2026-03-01&end_date=2026-03-30")
	require.Equal(t, http.StatusOK, rec.Code)
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 3, 31, 0, 0, 0, 0, loc), repo.query.RangeEndExclusive)
	require.True(t, repo.query.AsOf.After(repo.query.RangeEndExclusive))
}
