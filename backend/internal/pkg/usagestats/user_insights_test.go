package usagestats

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewUserInsightsQueryCalendarDaysAndCutoff(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, 10, 15, 0, 0, 0, 0, loc)
	asOf := time.Date(2026, 11, 1, 12, 30, 0, 123456789, loc)
	end := start.AddDate(0, 0, 30)
	f := UsageLogFilters{UserID: 42, StartTime: &start, EndTime: &end}
	q, err := NewUserInsightsQuery(f, start, end, asOf, loc)
	require.NoError(t, err)
	require.Equal(t, asOf, q.RangeEndExclusive)
	require.Nil(t, q.Filters.StartTime)
	require.Nil(t, q.Filters.EndTime)
	require.Equal(t, ModelSourceRequested, q.Filters.ModelFilterSource)
	require.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc), q.TodayStart)
	require.Equal(t, asOf, q.TodayEndExclusive)
	_, err = NewUserInsightsQuery(f, start, end.AddDate(0, 0, 1), asOf, loc)
	require.ErrorIs(t, err, ErrInvalidUserInsightsRange)
	// March's DST transition makes 30 calendar days less than 30*24 hours.
	spring := time.Date(2026, 3, 1, 0, 0, 0, 0, loc)
	_, err = NewUserInsightsQuery(f, spring, spring.AddDate(0, 0, 30), asOf, loc)
	require.NoError(t, err)
	// Fall-back's 25-hour day must still fit a 30-calendar-day range.
	fall := time.Date(2026, 10, 20, 0, 0, 0, 0, loc)
	_, err = NewUserInsightsQuery(f, fall, fall.AddDate(0, 0, 30), asOf.AddDate(0, 0, 30), loc)
	require.NoError(t, err)
}

func TestNewUserInsightsQueryRejectsInvalidBounds(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-time.Hour)
	for _, tc := range []struct {
		start, end time.Time
		uid        int64
	}{
		{now, now, 42}, {now.Add(time.Hour), now.Add(2 * time.Hour), 42},
		{start, now, 0}, {time.Time{}, now, 42}, {start, start.Add(-time.Hour), 42},
	} {
		_, err := NewUserInsightsQuery(UsageLogFilters{UserID: tc.uid}, tc.start, tc.end, now, time.UTC)
		require.ErrorIs(t, err, ErrInvalidUserInsightsRange)
	}
}

func TestUserKeyActivityJSONIsPublicAndPreservesNanoseconds(t *testing.T) {
	asOf := time.Date(2026, 11, 1, 12, 0, 0, 123456789, time.UTC)
	body, err := json.Marshal(UserKeyActivity{AsOf: asOf, Keys: []UserKeyInsight{{APIKeyID: 7}}, ActiveWindows: []UserActiveWindow{}})
	require.NoError(t, err)
	require.Contains(t, string(body), `2026-11-01T12:00:00.123456789Z`)
	require.Contains(t, string(body), `"favourite_model":null`)
	require.Contains(t, string(body), `"last_used_at":null`)
	require.NotContains(t, string(body), `account_cost`)
	require.NotContains(t, string(body), `"key":`)
}
