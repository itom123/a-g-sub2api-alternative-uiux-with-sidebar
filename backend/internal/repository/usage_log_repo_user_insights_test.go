package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func userInsightsTestQuery(t *testing.T) usagestats.UserInsightsQuery {
	t.Helper()
	asOf := time.Date(2026, 11, 1, 12, 0, 0, 123456789, time.UTC)
	q, err := usagestats.NewUserInsightsQuery(usagestats.UsageLogFilters{UserID: 42}, asOf.Add(-24*time.Hour), asOf, asOf, time.UTC)
	require.NoError(t, err)
	return q
}
func TestUserKeyActivitySQLCompleteScopedParameterized(t *testing.T) {
	q := userInsightsTestQuery(t)
	requestType := int16(service.RequestTypeWSV2)
	stream := false
	native := true
	billing := int8(1)
	mismatch := false
	q.Filters = usagestats.UsageLogFilters{UserID: 42, APIKeyID: 7, AccountID: 8, GroupID: 9, RequestID: "req-id", Model: "model' OR 1=1 --", RequestType: &requestType, Stream: &stream, NativeCompactionV2: &native, BillingType: &billing, BillingMode: "image", UpstreamModelMismatch: &mismatch}
	query, args := buildUserInsightsQuery(q)
	require.Contains(t, query, "user_id = $1")
	require.Contains(t, query, "k.user_id = $1")
	require.Contains(t, query, "FROM api_keys WHERE user_id = $1")
	require.Contains(t, query, "created_at < $2")
	require.Contains(t, query, "created_at >= $3 AND created_at < $4")
	require.Contains(t, query, `requested_model COLLATE "C" ASC`)
	require.Contains(t, query, "COUNT(*) DESC, SUM(tokens) DESC")
	require.Contains(t, query, "COALESCE(NULLIF(TRIM(requested_model), ''), model)")
	require.Contains(t, query, "account_id = $8")
	require.Contains(t, query, "group_id = $9")
	require.Contains(t, query, "request_id = $10")
	require.Contains(t, query, "native_compaction_v2 = $13")
	require.Contains(t, query, "billing_type = $14")
	require.Contains(t, query, "billing_mode = $15")
	require.NotContains(t, query, q.Filters.Model)
	require.NotContains(t, strings.ToUpper(query), "LIMIT ")
	require.NotContains(t, query, "actual_cost > 0")
	require.NotContains(t, query, "account_cost")
	require.NotContains(t, query, "stream = $13") // request_type wins over stream
	require.Equal(t, []any{int64(42), q.AsOf, q.RangeStart, q.RangeEndExclusive, q.TodayStart, q.TodayEndExclusive, int64(7), int64(8), int64(9), "req-id", q.Filters.Model, requestType, true, int16(1), "image"}, args)
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	values := make([]driver.Value, len(args))
	for i, v := range args {
		values[i] = v
	}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(values...).WillReturnRows(sqlmock.NewRows([]string{"keys", "windows"}).AddRow(`[{"api_key_id":7,"requests":6001,"total_tokens":123456,"lifetime_actual_cost":80,"favourite_model":"requested"}]`, `[{"id":"60m","requests":6001,"tokens":123456,"model_shares":[]}]`))
	got, err := repo.GetUserKeyActivity(context.Background(), q)
	require.NoError(t, err)
	require.Equal(t, int64(6001), got.Keys[0].Requests)
	require.Equal(t, q.AsOf, got.AsOf)
	require.Equal(t, "retained_logs", got.Coverage)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestUserKeyActivityErrorsNeverReturnFakeZero(t *testing.T) {
	for _, name := range []string{"query", "cancel", "scan", "json", "rows"} {
		t.Run(name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			q := userInsightsTestQuery(t)
			expect := mock.ExpectQuery("WITH scoped AS MATERIALIZED")
			ctx := context.Background()
			switch name {
			case "query":
				expect.WillReturnError(errors.New("database details are private"))
			case "cancel":
				expect.WillReturnError(context.Canceled)
			case "scan":
				expect.WillReturnRows(sqlmock.NewRows([]string{"keys"}).AddRow("[]"))
			case "json":
				expect.WillReturnRows(sqlmock.NewRows([]string{"keys", "windows"}).AddRow("bad json", "[]"))
			case "rows":
				expect.WillReturnRows(sqlmock.NewRows([]string{"keys", "windows"}).AddRow("[]", "[]").RowError(0, errors.New("row failure")))
			}
			got, err := repo.GetUserKeyActivity(ctx, q)
			require.Error(t, err)
			require.Nil(t, got)
			if name == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// Exports the actual generated statement for the isolated PostgreSQL-WASM
// verifier when native Docker is unavailable; ordinary test runs write nothing.
func TestUserKeyActivityExportPostgresQuery(t *testing.T) {
	output := os.Getenv("USER_INSIGHTS_QUERY_EXPORT")
	if output == "" {
		t.Skip("query export not requested")
	}
	asOf := time.Date(2026, 11, 3, 12, 0, 0, 123456000, time.UTC)
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	base, err := usagestats.NewUserInsightsQuery(usagestats.UsageLogFilters{UserID: 42}, start, asOf.Add(time.Hour), asOf, loc)
	require.NoError(t, err)
	queries := map[string]any{}
	for _, name := range []string{"base", "filtered", "historical", "foreign"} {
		q := base
		if name == "historical" {
			q.RangeStart = asOf.AddDate(0, 0, -50)
			q.RangeEndExclusive = asOf.AddDate(0, 0, -40)
		}
		if name == "filtered" {
			q.Filters.Model = "requested"
			billing := int8(1)
			q.Filters.BillingType = &billing
		}
		if name == "foreign" {
			q.Filters.APIKeyID = 99
		}
		query, args := buildUserInsightsQuery(q)
		queries[name] = map[string]any{"query": query, "args": args}
	}
	body, err := json.MarshalIndent(queries, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(output), 0700))
	require.NoError(t, os.WriteFile(output, body, 0600))
}

func TestUserKeyActivityRejectsUnscopedQuery(t *testing.T) {
	q := userInsightsTestQuery(t)
	q.Filters.UserID = 0
	got, err := (&usageLogRepository{}).GetUserKeyActivity(context.Background(), q)
	require.Nil(t, got)
	require.ErrorIs(t, err, usagestats.ErrInvalidUserInsightsRange)
}
