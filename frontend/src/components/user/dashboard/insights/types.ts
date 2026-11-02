import type { ApiKey, GroupStat, ModelStat, TrendDataPoint, UsageLog, UsageStatsResponse } from '@/types'
import type { ApiKeyDailyUsageResponse } from '@/api/usage'

export interface DashboardRange {
  startDate: string
  endDate: string
  timezone: string
  preset: 'today' | '7d' | 'thisWeek' | 'lastWeek' | 'thisMonth' | '30d' | 'custom'
}

export interface DashboardKeyUsageRow {
  apiKeyId: number
  keyName: string
  status: string
  selectedRangeCostUsd: number | null
  lifetimeCostUsd: number | null
  todayCostUsd: number | null
  requests: number | null
  inputTokens: number | null
  cacheCreationTokens: number | null
  cacheReadTokens: number | null
  outputTokens: number | null
  totalTokens: number | null
  favouriteModel: string | null
  lastUsedAt: string | null
  truncated: boolean
}

export interface ActiveNowWindow {
  id: '60m' | '12h' | '24h'
  requests: number | null
  tokens: number | null
  rpm: number | null
  tpm: number | null
  modelShares: Array<{ model: string; requests: number; share: number }>
  lastCallAt: string | null
  complete: boolean
  cappedPages: number | null
}

export interface DashboardConsistency {
  requests?: number | null
  activeDays30: number | null
  callsPerActiveDay: number | null
  tokensPerCall: number | null
  trend7d: 'up' | 'down' | 'steady' | 'no_data'
  recentCalls: number | null
  priorCalls: number | null
}

export interface DashboardRolling30d {
  startDate: string
  endDate: string
  actualCostUsd: number | null
  requests: number | null
  totalTokens: number | null
  activeDays: number | null
  callsPerActiveDay?: number | null
  tokensPerCall: number | null
}

export type DashboardHealth = 'all_good' | 'attention' | 'awaiting_data'

export interface DashboardErrorRow {
  id: number
  created_at: string
  model: string
  inbound_endpoint: string
  status_code: number
  category: string
  platform: string
  message: string
  key_name: string
}

export interface DashboardKeyBatchStats {
  api_key_id: number
  today_actual_cost: number
  total_actual_cost: number
}

export interface DashboardAggregationOptions {
  maxPages?: number
  pagesFetched?: number
  hasMore?: boolean
  now?: number
}

export interface DashboardKeyDrilldownState {
  keyInfo: ApiKey | null
  daily: ApiKeyDailyUsageResponse | null
  models: ModelStat[]
  loading: boolean
  dailyLoading: boolean
  modelsLoading: boolean
  dailyError: boolean
  modelsError: boolean
}

export type DashboardUsageStats = UsageStatsResponse
export type DashboardTrend = TrendDataPoint[]
export type DashboardModels = ModelStat[]
export type DashboardGroups = GroupStat[]
export type DashboardUsageRows = UsageLog[]
