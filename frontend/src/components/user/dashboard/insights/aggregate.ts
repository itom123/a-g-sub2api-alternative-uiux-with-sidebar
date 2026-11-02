import type { ApiKey, TrendDataPoint, UsageLog, UsageStatsResponse } from '@/types'
import type {
  ActiveNowWindow,
  DashboardAggregationOptions,
  DashboardConsistency,
  DashboardHealth,
  DashboardKeyBatchStats,
  DashboardKeyUsageRow,
  DashboardRolling30d,
} from './types'

const finite = (value: unknown): number | null => {
  const number = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(number) ? number : null
}
const numberOrZero = (value: unknown): number => finite(value) ?? 0
const dateMs = (value: unknown): number | null => {
  if (typeof value !== 'string' && !(value instanceof Date)) return null
  const result = new Date(value).getTime()
  return Number.isFinite(result) ? result : null
}
const rowTokens = (row: UsageLog): number => numberOrZero(row.input_tokens) + numberOrZero(row.cache_creation_tokens) + numberOrZero(row.cache_read_tokens) + numberOrZero(row.output_tokens)

export function aggregateKeyUsage(
  rows: UsageLog[],
  keys: Pick<ApiKey, 'id' | 'name' | 'status' | 'last_used_at'>[],
  batchStats: Record<string, DashboardKeyBatchStats> = {},
  options: DashboardAggregationOptions = {},
): DashboardKeyUsageRow[] {
  const grouped = new Map<number, { requests: number; input: number; creation: number; read: number; output: number; cost: number; tokens: number; last: string | null; models: Map<string, { requests: number; tokens: number }> }>()
  for (const row of rows || []) {
    const id = numberOrZero(row.api_key_id)
    if (!id) continue
    const current = grouped.get(id) ?? { requests: 0, input: 0, creation: 0, read: 0, output: 0, cost: 0, tokens: 0, last: null, models: new Map() }
    current.requests += 1
    current.input += numberOrZero(row.input_tokens)
    current.creation += numberOrZero(row.cache_creation_tokens)
    current.read += numberOrZero(row.cache_read_tokens)
    current.output += numberOrZero(row.output_tokens)
    current.tokens += rowTokens(row)
    current.cost += numberOrZero(row.actual_cost)
    const timestamp = typeof row.created_at === 'string' ? row.created_at : null
    if (timestamp && (!current.last || (dateMs(timestamp) ?? -Infinity) > (dateMs(current.last) ?? -Infinity))) current.last = timestamp
    const model = typeof row.model === 'string' && row.model ? row.model : ''
    const modelStats = current.models.get(model) ?? { requests: 0, tokens: 0 }
    modelStats.requests += 1
    modelStats.tokens += rowTokens(row)
    current.models.set(model, modelStats)
    grouped.set(id, current)
  }
  const truncated = options.hasMore ?? ((options.pagesFetched ?? 0) >= (options.maxPages ?? 5))
  return keys.map((key) => {
    const stats = grouped.get(key.id)
    let favouriteModel: string | null = null
    if (stats?.models.size) {
      favouriteModel = [...stats.models.entries()].sort((a, b) => b[1].requests - a[1].requests || b[1].tokens - a[1].tokens || a[0].localeCompare(b[0]))[0]?.[0] || null
    }
    const batch = batchStats[String(key.id)]
    return {
      apiKeyId: key.id,
      keyName: key.name || `#${key.id}`,
      status: key.status,
      selectedRangeCostUsd: stats ? stats.cost : null,
      lifetimeCostUsd: finite(batch?.total_actual_cost),
      todayCostUsd: finite(batch?.today_actual_cost),
      requests: stats ? stats.requests : null,
      inputTokens: stats ? stats.input : null,
      cacheCreationTokens: stats ? stats.creation : null,
      cacheReadTokens: stats ? stats.read : null,
      outputTokens: stats ? stats.output : null,
      totalTokens: stats ? stats.tokens : null,
      favouriteModel,
      lastUsedAt: stats?.last || key.last_used_at || null,
      truncated,
    }
  })
}

export function aggregateActiveNow(rows: UsageLog[], options: DashboardAggregationOptions = {}): ActiveNowWindow[] {
  const now = options.now ?? Date.now()
  const windows: Array<ActiveNowWindow['id']> = ['60m', '12h', '24h']
  const durations: Record<ActiveNowWindow['id'], number> = { '60m': 60 * 60 * 1000, '12h': 12 * 60 * 60 * 1000, '24h': 24 * 60 * 60 * 1000 }
  const complete = !(options.hasMore ?? ((options.pagesFetched ?? 0) >= (options.maxPages ?? 5)))
  return windows.map((id) => {
    const from = now - durations[id]
    const matching = (rows || []).filter((row) => {
      const timestamp = dateMs(row.created_at)
      return timestamp !== null && timestamp >= from && timestamp <= now
    })
    const modelCounts = new Map<string, number>()
    let lastCallAt: string | null = null
    let tokens = 0
    for (const row of matching) {
      const model = row.model || 'unknown'
      modelCounts.set(model, (modelCounts.get(model) || 0) + 1)
      tokens += rowTokens(row)
      if (!lastCallAt || (dateMs(row.created_at) ?? -Infinity) > (dateMs(lastCallAt) ?? -Infinity)) lastCallAt = row.created_at
    }
    const requests = matching.length
    const minutes = durations[id] / 60000
    return { id, requests, tokens, rpm: requests / minutes, tpm: tokens / minutes, modelShares: [...modelCounts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([model, count]) => ({ model, requests: count, share: requests ? count / requests : 0 })), lastCallAt, complete, cappedPages: complete ? null : (options.pagesFetched ?? options.maxPages ?? 5) }
  })
}

export function aggregateConsistency(trend: TrendDataPoint[]): DashboardConsistency {
  const daily = (trend || []).filter((point) => finite(point.requests) !== null).slice(-30)
  const requests = daily.reduce((sum, point) => sum + numberOrZero(point.requests), 0)
  const tokens = daily.reduce((sum, point) => sum + numberOrZero(point.total_tokens), 0)
  const activeDays = daily.filter((point) => numberOrZero(point.requests) > 0).length
  const recent = daily.slice(-7).reduce((sum, point) => sum + numberOrZero(point.requests), 0)
  const prior = daily.slice(-14, -7).reduce((sum, point) => sum + numberOrZero(point.requests), 0)
  let trend7d: DashboardConsistency['trend7d'] = 'no_data'
  if (recent || prior) trend7d = prior > 0 && recent / prior >= 1.25 ? 'up' : prior > 0 && recent / prior <= 0.8 ? 'down' : 'steady'
  return { requests: daily.length ? requests : null, activeDays30: activeDays || (daily.length ? 0 : null), callsPerActiveDay: activeDays ? requests / activeDays : (daily.length ? 0 : null), tokensPerCall: requests ? Math.round(tokens / requests) : (daily.length ? 0 : null), trend7d, recentCalls: daily.length ? recent : null, priorCalls: daily.length ? prior : null }
}

export function aggregateRolling30d(startDate: string, endDate: string, stats: UsageStatsResponse | null, trend: TrendDataPoint[]): DashboardRolling30d {
  const daily = (trend || []).filter((point) => numberOrZero(point.requests) > 0)
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

export function deriveDashboardHealth(input: { loaded: boolean; stale?: boolean; lowBalance?: boolean; truncated?: boolean; quotaAttention?: boolean; requiredFailed?: boolean }): DashboardHealth {
  // A complete response with zero usage is valid, but missing required response
  // fields means the dashboard cannot make a trustworthy health claim yet.
  if (!input.loaded || input.requiredFailed) return 'awaiting_data'
  return input.stale || input.lowBalance || input.truncated || input.quotaAttention ? 'attention' : 'all_good'
}
