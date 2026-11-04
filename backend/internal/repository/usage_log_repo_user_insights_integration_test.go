//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Uses the existing disposable PostgreSQL/Redis harness, never production.
func TestUserKeyActivityCompleteAggregationIntegration(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "key-insights@test.com", Balance: 123})
	other := mustCreateUser(t, client, &service.User{Email: "other-key-insights@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-insights-used", Name: "Used", Quota: 90, QuotaUsed: 12})
	deleted := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-insights-deleted", Name: "Deleted"})
	tie := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-insights-tie", Name: "Tie"})
	foreign := mustCreateApiKey(t, client, &service.APIKey{UserID: other.ID, Key: "sk-insights-other-secret", Name: "Other private name"})
	account := mustCreateAccount(t, client, &service.Account{Name: "insights-account"})
	asOf := time.Date(2026, 11, 3, 12, 0, 0, 123456000, time.UTC)
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	q, err := usagestats.NewUserInsightsQuery(usagestats.UsageLogFilters{UserID: user.ID}, start, asOf.Add(time.Hour), asOf, loc)
	require.NoError(t, err)
	// More keys and logs than either old client ceiling, in a bounded bulk fixture.
	_, err = tx.ExecContext(ctx, `INSERT INTO api_keys (user_id,key,name,status) SELECT $1,'sk-insights-zero-'||n,'Zero '||n,'active' FROM generate_series(1,1005) n`, user.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_logs (user_id,api_key_id,account_id,model,requested_model,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,actual_cost,billing_type,created_at)
 SELECT $1,$2,$3,'fallback','requested',1,2,3,4,0,n%2,$4::timestamptz FROM generate_series(1,6002) n`, user.ID, key.ID, account.ID, asOf.Add(-30*time.Minute))
	require.NoError(t, err)
	insert := func(uid, kid int64, when time.Time, cost float64, model string, tokens int, billing int) {
		_, insertErr := tx.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,model,requested_model,input_tokens,actual_cost,billing_type,created_at) VALUES($1,$2,$3,$4,'',$5,$6,$7,$8)`, uid, kid, account.ID, model, tokens, cost, billing, when)
		require.NoError(t, insertErr)
	}
	insert(user.ID, key.ID, asOf.AddDate(0, 0, -45), 7, "old-retained", 1, 0)
	insert(user.ID, key.ID, q.TodayStart, 2, "midnight", 1, 1)
	insert(user.ID, key.ID, asOf.Add(-60*time.Minute), 3, "boundary-60m", 1, 0)
	insert(user.ID, key.ID, asOf.Add(-12*time.Hour), 4, "boundary-12h", 1, 1)
	insert(user.ID, key.ID, asOf.Add(-24*time.Hour), 5, "boundary-24h", 1, 0)
	insert(user.ID, key.ID, asOf, 99, "cutoff-excluded", 100, 0)
	insert(user.ID, key.ID, asOf.Add(time.Second), 100, "future-excluded", 100, 0)
	insert(user.ID, deleted.ID, asOf.Add(-time.Minute), 0, "deleted-free", 0, 1)
	insert(other.ID, foreign.ID, asOf.Add(-time.Minute), 9999, "other-secret-model", 999, 0)
	// Equal requests: tokens win, then name wins. b wins count over a-large.
	for _, model := range []string{"a-large", "b", "b", "c", "c"} {
		tokens := 2
		if model == "a-large" {
			tokens = 100
		}
		insert(user.ID, tie.ID, asOf.Add(-time.Minute), 0, model, tokens, 0)
	}
	_, err = tx.ExecContext(ctx, `UPDATE api_keys SET deleted_at=$2 WHERE id=$1 AND user_id=$3`, deleted.ID, asOf, user.ID)
	require.NoError(t, err)
	var beforeBalance, beforeQuota float64
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT u.balance,k.quota_used FROM users u JOIN api_keys k ON k.user_id=u.id WHERE u.id=$1 AND k.id=$2`, []any{user.ID, key.ID}, &beforeBalance, &beforeQuota))
	got, err := repo.GetUserKeyActivity(ctx, q)
	require.NoError(t, err)
	require.Len(t, got.Keys, 1008) // 1005 zero-use + used + tie + deleted
	byID := map[int64]usagestats.UserKeyInsight{}
	for _, k := range got.Keys {
		byID[k.APIKeyID] = k
	}
	used := byID[key.ID]
	require.Equal(t, int64(6006), used.Requests)
	require.Equal(t, int64(60024), used.TotalTokens)
	require.Equal(t, int64(6006), used.InputTokens)
	require.Equal(t, int64(12004), used.OutputTokens)
	require.Equal(t, int64(18006), used.CacheCreationTokens)
	require.Equal(t, int64(24008), used.CacheReadTokens)
	require.Equal(t, 14.0, used.RangeActualCost)
	require.Equal(t, 21.0, used.LifetimeActualCost)
	require.Equal(t, 5.0, used.TodayActualCost)
	require.Equal(t, "requested", *used.FavouriteModel)
	require.True(t, used.LastUsedAt.Equal(asOf.Add(-30*time.Minute)))
	require.Equal(t, "b", *byID[tie.ID].FavouriteModel)
	require.True(t, byID[deleted.ID].Deleted)
	require.Equal(t, "deleted", byID[deleted.ID].Status)
	require.Equal(t, int64(1), byID[deleted.ID].Requests)
	require.NotContains(t, byID, foreign.ID)
	zeroCount := 0
	for _, k := range got.Keys {
		if k.Requests == 0 {
			zeroCount++
			require.Zero(t, k.LifetimeActualCost)
			require.Nil(t, k.FavouriteModel)
		}
	}
	require.Equal(t, 1005, zeroCount)
	windows := got.ActiveWindows
	require.Len(t, windows, 3)
	require.Equal(t, []string{"60m", "12h", "24h"}, []string{windows[0].ID, windows[1].ID, windows[2].ID})
	require.Equal(t, int64(6009), windows[0].Requests) // bulk + exact60m + deleted + 5 tie
	require.Equal(t, int64(6011), windows[1].Requests) // + local midnight + exact12h
	require.Equal(t, int64(6012), windows[2].Requests) // + exact24h
	require.InDelta(t, float64(windows[0].Requests)/60, windows[0].RPM, 1e-10)
	require.InDelta(t, float64(windows[0].Tokens)/60, windows[0].TPM, 1e-10)
	require.True(t, windows[0].LastCallAt.Equal(asOf.Add(-time.Minute)))
	for _, w := range windows {
		sum := 0.0
		for _, m := range w.ModelShares {
			sum += m.Share
		}
		require.InDelta(t, 1.0, sum, 1e-10)
	}
	body, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(body), "other-secret")
	require.NotContains(t, string(body), "sk-insights")
	require.NotContains(t, string(body), "account_cost")
	// Selected historical dates do not alter live windows, lifetime or today's cost.
	historical := q
	historical.RangeStart = asOf.AddDate(0, 0, -50)
	historical.RangeEndExclusive = asOf.AddDate(0, 0, -40)
	old, err := repo.GetUserKeyActivity(ctx, historical)
	require.NoError(t, err)
	require.Equal(t, windows, old.ActiveWindows)
	// Optional filters apply to every log-derived branch, including lifetime/windows.
	q.Filters.Model = "requested"
	q.Filters.BillingType = new(int8)
	*q.Filters.BillingType = 1
	filtered, err := repo.GetUserKeyActivity(ctx, q)
	require.NoError(t, err)
	for _, k := range filtered.Keys {
		if k.APIKeyID == key.ID {
			require.Equal(t, int64(3001), k.Requests)
			require.Zero(t, k.RangeActualCost)
			require.Zero(t, k.LifetimeActualCost)
		}
	}
	require.Equal(t, int64(3001), filtered.ActiveWindows[0].Requests)
	q.Filters.APIKeyID = foreign.ID
	scoped, err := repo.GetUserKeyActivity(ctx, q)
	require.NoError(t, err)
	require.Empty(t, scoped.Keys)
	require.Zero(t, scoped.ActiveWindows[0].Requests)
	var afterBalance, afterQuota float64
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT u.balance,k.quota_used FROM users u JOIN api_keys k ON k.user_id=u.id WHERE u.id=$1 AND k.id=$2`, []any{user.ID, key.ID}, &afterBalance, &afterQuota))
	require.Equal(t, beforeBalance, afterBalance)
	require.Equal(t, beforeQuota, afterQuota)
}
