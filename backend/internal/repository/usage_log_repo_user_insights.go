package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// GetUserKeyActivity aggregates all retained matching logs in one PostgreSQL
// statement/read snapshot. There is deliberately no raw-row or API-key limit,
// rollup watermark, positive-cost proxy, billing write or quota mutation here.
func (r *usageLogRepository) GetUserKeyActivity(ctx context.Context, q usagestats.UserInsightsQuery) (*usagestats.UserKeyActivity, error) {
	if q.Filters.UserID <= 0 || q.AsOf.IsZero() || !q.RangeStart.Before(q.RangeEndExclusive) || q.RangeEndExclusive.After(q.AsOf) || q.TodayEndExclusive.After(q.AsOf) {
		return nil, usagestats.ErrInvalidUserInsightsRange
	}
	query, args := buildUserInsightsQuery(q)
	var keysJSON, windowsJSON []byte
	if err := scanSingleRow(ctx, r.sql, query, args, &keysJSON, &windowsJSON); err != nil {
		return nil, fmt.Errorf("query user key activity: %w", err)
	}
	result := &usagestats.UserKeyActivity{
		AsOf: q.AsOf, RangeStart: q.RangeStart, RangeEndExclusive: q.RangeEndExclusive,
		Coverage: "retained_logs", Keys: []usagestats.UserKeyInsight{}, ActiveWindows: []usagestats.UserActiveWindow{},
	}
	if err := json.Unmarshal(keysJSON, &result.Keys); err != nil {
		return nil, fmt.Errorf("decode user key activity: %w", err)
	}
	if err := json.Unmarshal(windowsJSON, &result.ActiveWindows); err != nil {
		return nil, fmt.Errorf("decode user active windows: %w", err)
	}
	return result, nil
}

func buildUserInsightsQuery(q usagestats.UserInsightsQuery) (string, []any) {
	f := q.Filters
	args := []any{f.UserID, q.AsOf, q.RangeStart, q.RangeEndExclusive, q.TodayStart, q.TodayEndExclusive, f.APIKeyID}
	conditions := []string{"user_id = $1", "created_at < $2", "($7::bigint = 0 OR api_key_id = $7)"}
	for _, filter := range []struct {
		column string
		value  int64
	}{{"account_id", f.AccountID}, {"group_id", f.GroupID}} {
		if filter.value > 0 {
			conditions = append(conditions, fmt.Sprintf("%s = $%d", filter.column, len(args)+1))
			args = append(args, filter.value)
		}
	}
	if f.RequestID != "" {
		conditions = append(conditions, fmt.Sprintf("request_id = $%d", len(args)+1))
		args = append(args, f.RequestID)
	}
	conditions, args = appendUsageLogModelWhereCondition(conditions, args, f.Model, usagestats.ModelSourceRequested)
	conditions, args = appendRequestTypeOrStreamWhereCondition(conditions, args, f.RequestType, f.Stream)
	conditions, args = appendNativeCompactionV2WhereCondition(conditions, args, f.NativeCompactionV2, "")
	if f.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("billing_type = $%d", len(args)+1))
		args = append(args, int16(*f.BillingType))
	}
	conditions, args = appendUsageLogBillingModeWhereCondition(conditions, args, f.BillingMode)
	if f.UpstreamModelMismatch != nil {
		conditions = append(conditions, upstreamModelMismatchCondition("upstream_model_mismatch", *f.UpstreamModelMismatch))
	}
	query := `
WITH scoped AS MATERIALIZED (
 SELECT api_key_id, created_at, actual_cost, input_tokens::bigint AS input_tokens,
  cache_creation_tokens::bigint AS cache_creation_tokens, cache_read_tokens::bigint AS cache_read_tokens,
  output_tokens::bigint AS output_tokens,
  input_tokens::bigint + cache_creation_tokens::bigint + cache_read_tokens::bigint + output_tokens::bigint AS tokens,
  ` + resolveModelDimensionExpression(usagestats.ModelSourceRequested) + ` AS requested_model
 FROM usage_logs WHERE ` + strings.Join(conditions, " AND ") + `
), key_ids AS (
 SELECT id AS api_key_id FROM api_keys WHERE user_id = $1 AND deleted_at IS NULL AND ($7::bigint = 0 OR id = $7)
 UNION SELECT api_key_id FROM scoped
), lifetime AS (
 SELECT api_key_id, SUM(actual_cost) AS lifetime_actual_cost,
  COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $5 AND created_at < $6), 0) AS today_actual_cost
 FROM scoped GROUP BY api_key_id
), ranged AS MATERIALIZED (
 SELECT * FROM scoped WHERE created_at >= $3 AND created_at < $4
), range_stats AS (
 SELECT api_key_id, COUNT(*) AS requests, SUM(actual_cost) AS range_actual_cost,
  SUM(input_tokens) AS input_tokens, SUM(cache_creation_tokens) AS cache_creation_tokens,
  SUM(cache_read_tokens) AS cache_read_tokens, SUM(output_tokens) AS output_tokens,
  SUM(tokens) AS total_tokens, MAX(created_at) AS last_used_at
 FROM ranged GROUP BY api_key_id
), favourites AS (
 SELECT api_key_id, requested_model, ROW_NUMBER() OVER (
  PARTITION BY api_key_id ORDER BY COUNT(*) DESC, SUM(tokens) DESC, requested_model COLLATE "C" ASC
 ) AS rank FROM ranged GROUP BY api_key_id, requested_model
), key_rows AS (
 SELECT ids.api_key_id, COALESCE(k.name, 'Historical key') AS key_name,
  CASE WHEN k.id IS NULL OR k.deleted_at IS NOT NULL THEN 'deleted' ELSE k.status END AS status,
  (k.id IS NULL OR k.deleted_at IS NOT NULL) AS deleted,
  COALESCE(r.range_actual_cost, 0) AS range_actual_cost,
  COALESCE(l.lifetime_actual_cost, 0) AS lifetime_actual_cost,
  COALESCE(l.today_actual_cost, 0) AS today_actual_cost,
  COALESCE(r.requests, 0) AS requests, COALESCE(r.input_tokens, 0) AS input_tokens,
  COALESCE(r.cache_creation_tokens, 0) AS cache_creation_tokens, COALESCE(r.cache_read_tokens, 0) AS cache_read_tokens,
  COALESCE(r.output_tokens, 0) AS output_tokens, COALESCE(r.total_tokens, 0) AS total_tokens,
  fav.requested_model AS favourite_model, r.last_used_at
 FROM key_ids ids
 LEFT JOIN api_keys k ON k.id = ids.api_key_id AND k.user_id = $1
 LEFT JOIN lifetime l ON l.api_key_id = ids.api_key_id
 LEFT JOIN range_stats r ON r.api_key_id = ids.api_key_id
 LEFT JOIN favourites fav ON fav.api_key_id = ids.api_key_id AND fav.rank = 1
), windows(id, minutes, duration, position) AS (
 VALUES ('60m', 60, INTERVAL '60 minutes', 1), ('12h', 720, INTERVAL '12 hours', 2), ('24h', 1440, INTERVAL '24 hours', 3)
), window_models AS (
 SELECT w.id, s.requested_model AS model, COUNT(*) AS requests, SUM(s.tokens) AS tokens, MAX(s.created_at) AS last_call_at
 FROM windows w JOIN scoped s ON s.created_at >= $2::timestamptz - w.duration AND s.created_at < $2
 GROUP BY w.id, s.requested_model
), window_totals AS (
 SELECT id, SUM(requests) AS requests, SUM(tokens) AS tokens, MAX(last_call_at) AS last_call_at
 FROM window_models GROUP BY id
), window_rows AS (
 SELECT w.id, w.position, COALESCE(t.requests, 0) AS requests, COALESCE(t.tokens, 0) AS tokens,
  COALESCE(t.requests, 0)::double precision / w.minutes AS rpm,
  COALESCE(t.tokens, 0)::double precision / w.minutes AS tpm, t.last_call_at,
  COALESCE((SELECT jsonb_agg(jsonb_build_object('model', m.model, 'requests', m.requests, 'tokens', m.tokens,
   'share', m.requests::double precision / t.requests) ORDER BY m.requests DESC, m.tokens DESC, m.model COLLATE "C" ASC)
   FROM window_models m WHERE m.id = w.id), '[]'::jsonb) AS model_shares
 FROM windows w LEFT JOIN window_totals t ON t.id = w.id
)
SELECT COALESCE((SELECT jsonb_agg(to_jsonb(k) ORDER BY k.api_key_id) FROM key_rows k), '[]'::jsonb),
 (SELECT jsonb_agg(to_jsonb(w) - 'position' ORDER BY w.position) FROM window_rows w)
`
	return query, args
}
