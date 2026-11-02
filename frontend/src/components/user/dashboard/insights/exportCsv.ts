import type { ModelStat, UsageStatsResponse } from '@/types'
import type { DashboardKeyUsageRow, DashboardRange } from './types'

function csvCell(value: unknown): string {
  const text = value === null || value === undefined ? '' : String(value)
  return /[",\n\r]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text
}

export function buildDashboardCsv(input: { range: DashboardRange; rangeStats: UsageStatsResponse | null; models: ModelStat[]; keys: DashboardKeyUsageRow[] }): string {
  const lines: string[] = []
  const push = (values: unknown[]) => lines.push(values.map(csvCell).join(','))
  push(['section', 'range_start', 'range_end', 'timezone', 'model', 'key', 'requests', 'input_tokens', 'cache_creation_tokens', 'cache_read_tokens', 'output_tokens', 'total_tokens', 'actual_cost_usd', 'standard_cost_usd', 'favourite_model', 'last_used_at'])
  push(['range', input.range.startDate, input.range.endDate, input.range.timezone, '', '', input.rangeStats?.total_requests ?? '', input.rangeStats?.total_input_tokens ?? '', input.rangeStats?.total_cache_creation_tokens ?? '', input.rangeStats?.total_cache_read_tokens ?? '', input.rangeStats?.total_output_tokens ?? '', input.rangeStats?.total_tokens ?? '', input.rangeStats?.total_actual_cost ?? '', input.rangeStats?.total_cost ?? '', '', ''])
  for (const model of input.models || []) push(['model', input.range.startDate, input.range.endDate, input.range.timezone, model.model, '', model.requests, model.input_tokens, model.cache_creation_tokens, model.cache_read_tokens, model.output_tokens, model.total_tokens, model.actual_cost, model.cost, '', ''])
  for (const key of input.keys || []) push(['key', input.range.startDate, input.range.endDate, input.range.timezone, '', key.keyName, key.requests ?? '', key.inputTokens ?? '', key.cacheCreationTokens ?? '', key.cacheReadTokens ?? '', key.outputTokens ?? '', key.totalTokens ?? '', key.selectedRangeCostUsd ?? '', '', key.favouriteModel ?? '', key.lastUsedAt ?? ''])
  const bom = String.fromCharCode(0xfeff)
  return `${bom}${lines.join('\r\n')}\r\n`
}

export function exportDashboardCsv(input: { range: DashboardRange; rangeStats: UsageStatsResponse | null; models: ModelStat[]; keys: DashboardKeyUsageRow[] }): void {
  const blob = new Blob([buildDashboardCsv(input)], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `account-usage-${input.range.startDate}-${input.range.endDate}.csv`
  anchor.click()
  URL.revokeObjectURL(url)
}
