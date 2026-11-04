package usagestats

import (
	"errors"
	"time"
)

var ErrInvalidUserInsightsRange = errors.New("key activity requires a valid user and a range of at most 30 calendar days")

// UserInsightsQuery is a read-only, current-user aggregation request. Dates only
// constrain range metrics; lifetime, today and active windows use their own bounds.
type UserInsightsQuery struct {
	Filters           UsageLogFilters
	AsOf              time.Time
	RangeStart        time.Time
	RangeEndExclusive time.Time
	TodayStart        time.Time
	TodayEndExclusive time.Time
}

func NewUserInsightsQuery(filters UsageLogFilters, start, end, asOf time.Time, loc *time.Location) (UserInsightsQuery, error) {
	if loc == nil || filters.UserID <= 0 || filters.APIKeyID < 0 || filters.AccountID < 0 || filters.GroupID < 0 || start.IsZero() || end.IsZero() || asOf.IsZero() || !start.Before(end) {
		return UserInsightsQuery{}, ErrInvalidUserInsightsRange
	}
	localStart := start.In(loc)
	dayStart := time.Date(localStart.Year(), localStart.Month(), localStart.Day(), 0, 0, 0, 0, loc)
	if end.After(dayStart.AddDate(0, 0, 30)) || !start.Before(asOf) {
		return UserInsightsQuery{}, ErrInvalidUserInsightsRange
	}
	if end.After(asOf) {
		end = asOf
	}
	now := asOf.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	// Strip date filters so they cannot accidentally truncate cumulative/window data.
	filters.StartTime, filters.EndTime = nil, nil
	filters.ModelFilterSource = ModelSourceRequested
	return UserInsightsQuery{Filters: filters, AsOf: asOf, RangeStart: start, RangeEndExclusive: end, TodayStart: today, TodayEndExclusive: asOf}, nil
}

// UserKeyActivity contains no API key secrets or upstream account costs. Time
// values use time.Time's RFC3339Nano JSON encoder, preserving subsecond cutoffs.
type UserKeyActivity struct {
	AsOf              time.Time          `json:"as_of"`
	RangeStart        time.Time          `json:"range_start"`
	RangeEndExclusive time.Time          `json:"range_end_exclusive"`
	Coverage          string             `json:"coverage"`
	Keys              []UserKeyInsight   `json:"keys"`
	ActiveWindows     []UserActiveWindow `json:"active_windows"`
}

type UserKeyInsight struct {
	APIKeyID            int64      `json:"api_key_id"`
	KeyName             string     `json:"key_name"`
	Status              string     `json:"status"`
	Deleted             bool       `json:"deleted"`
	RangeActualCost     float64    `json:"range_actual_cost"`
	LifetimeActualCost  float64    `json:"lifetime_actual_cost"`
	TodayActualCost     float64    `json:"today_actual_cost"`
	Requests            int64      `json:"requests"`
	InputTokens         int64      `json:"input_tokens"`
	CacheCreationTokens int64      `json:"cache_creation_tokens"`
	CacheReadTokens     int64      `json:"cache_read_tokens"`
	OutputTokens        int64      `json:"output_tokens"`
	TotalTokens         int64      `json:"total_tokens"`
	FavouriteModel      *string    `json:"favourite_model"`
	LastUsedAt          *time.Time `json:"last_used_at"`
}

type UserActiveWindow struct {
	ID          string                 `json:"id"`
	Requests    int64                  `json:"requests"`
	Tokens      int64                  `json:"tokens"`
	RPM         float64                `json:"rpm"`
	TPM         float64                `json:"tpm"`
	LastCallAt  *time.Time             `json:"last_call_at"`
	ModelShares []UserActiveModelShare `json:"model_shares"`
}

type UserActiveModelShare struct {
	Model    string  `json:"model"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Share    float64 `json:"share"` // Fraction of requests in this window, not percent.
}
