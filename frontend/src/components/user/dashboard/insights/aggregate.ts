import type { TrendDataPoint, UsageStatsResponse } from '@/types'
import type { DashboardConsistency, DashboardHealth, DashboardRolling30d } from './types'

const finite = (value: unknown): number | null => value == null ? null : Number.isFinite(Number(value)) ? Number(value) : null
const numberOrZero = (value: unknown): number => finite(value) ?? 0

export function aggregateConsistency(trend: TrendDataPoint[]): DashboardConsistency {
  const daily = (trend || []).filter(point => finite(point.requests) !== null).slice(-30)
  const requests = daily.reduce((sum, point) => sum + numberOrZero(point.requests), 0)
  const tokens = daily.reduce((sum, point) => sum + numberOrZero(point.total_tokens), 0)
  const activeDays = daily.filter(point => numberOrZero(point.requests) > 0).length
  const recent = daily.slice(-7).reduce((sum, point) => sum + numberOrZero(point.requests), 0)
  const prior = daily.slice(-14, -7).reduce((sum, point) => sum + numberOrZero(point.requests), 0)
  let trend7d: DashboardConsistency['trend7d'] = 'no_data'
  if (recent || prior) trend7d = prior > 0 && recent / prior >= 1.25 ? 'up' : prior > 0 && recent / prior <= 0.8 ? 'down' : 'steady'
  return { requests: daily.length ? requests : null, activeDays30: activeDays || (daily.length ? 0 : null), callsPerActiveDay: activeDays ? requests / activeDays : (daily.length ? 0 : null), tokensPerCall: requests ? Math.round(tokens / requests) : (daily.length ? 0 : null), trend7d, recentCalls: daily.length ? recent : null, priorCalls: daily.length ? prior : null }
}

export function aggregateRolling30d(startDate: string, endDate: string, stats: UsageStatsResponse | null, trend: TrendDataPoint[]): DashboardRolling30d {
  const daily = (trend || []).filter(point => numberOrZero(point.requests) > 0)
  const trendRequests = (trend || []).reduce((sum, point) => sum + numberOrZero(point.requests), 0)
  const trendTokens = (trend || []).reduce((sum, point) => sum + numberOrZero(point.total_tokens), 0)
  const requests = stats ? finite(stats.total_requests) : (trend?.length ? trendRequests : null)
  const totalTokens = stats ? finite(stats.total_tokens) : (trend?.length ? trendTokens : null)
  const trendCost = (trend || []).reduce((sum, point) => sum + numberOrZero(point.actual_cost), 0)
  return { startDate, endDate, actualCostUsd: stats ? finite(stats.total_actual_cost) : (trend?.length ? trendCost : null), requests, totalTokens, activeDays: trend?.length ? daily.length : null, tokensPerCall: requests && requests > 0 && totalTokens !== null ? totalTokens / requests : requests === 0 ? 0 : null }
}

export function aggregateRollingAndConsistency(startDate: string, endDate: string, stats: UsageStatsResponse | null, trend: TrendDataPoint[]): { rolling30: DashboardRolling30d; consistency: DashboardConsistency } {
  const rolling30 = aggregateRolling30d(startDate, endDate, stats, trend)
  const consistency = aggregateConsistency(trend)
  const tokensPerCall = rolling30.requests && rolling30.requests > 0 && rolling30.totalTokens !== null ? Math.round(rolling30.totalTokens / rolling30.requests) : rolling30.requests === 0 ? 0 : null
  return { rolling30: { ...rolling30, tokensPerCall, callsPerActiveDay: rolling30.activeDays && rolling30.requests !== null ? rolling30.requests / rolling30.activeDays : rolling30.activeDays === 0 ? 0 : null }, consistency: { ...consistency, requests: rolling30.requests, activeDays30: rolling30.activeDays, callsPerActiveDay: rolling30.activeDays && rolling30.requests !== null ? rolling30.requests / rolling30.activeDays : rolling30.activeDays === 0 ? 0 : null, tokensPerCall } }
}

export function deriveDashboardHealth(input: { loaded: boolean; stale?: boolean; lowBalance?: boolean; quotaAttention?: boolean; requiredFailed?: boolean }): DashboardHealth {
  if (!input.loaded || input.requiredFailed) return 'awaiting_data'
  return input.stale || input.lowBalance || input.quotaAttention ? 'attention' : 'all_good'
}
