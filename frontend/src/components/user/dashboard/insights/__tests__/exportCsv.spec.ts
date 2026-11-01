import { describe, expect, it } from 'vitest'
import { buildDashboardCsv } from '../exportCsv'
import type { DashboardKeyUsageRow, DashboardRange } from '../types'

const range: DashboardRange = { startDate: '2026-10-01', endDate: '2026-10-31', timezone: 'Asia/Shanghai', preset: '30d' }
const key: DashboardKeyUsageRow = { apiKeyId: 1, keyName: 'key, "quoted"', status: 'active', selectedRangeCostUsd: 1, lifetimeCostUsd: null, todayCostUsd: null, requests: 2, inputTokens: 3, cacheCreationTokens: 4, cacheReadTokens: 5, outputTokens: 6, totalTokens: 18, favouriteModel: 'model\nwith newline', lastUsedAt: null, truncated: false }

describe('buildDashboardCsv', () => {
  it('prepends a BOM, has stable headers, escapes RFC 4180 fields, and uses only current range data', () => {
    const csv = buildDashboardCsv({ range, rangeStats: { total_requests: 2, total_input_tokens: 3, total_output_tokens: 6, total_cache_tokens: 9, total_cache_read_tokens: 5, total_cache_creation_tokens: 4, total_tokens: 18, total_cost: 2, total_actual_cost: 1, average_duration_ms: 10 }, models: [{ model: 'm,odel', requests: 2, input_tokens: 3, output_tokens: 6, cache_creation_tokens: 4, cache_read_tokens: 5, total_tokens: 18, cost: 2, actual_cost: 1, account_cost: 99 }], keys: [key] })
    expect(csv.charCodeAt(0)).toBe(0xfeff)
    expect(csv).toContain('section,range_start,range_end,timezone,model,key,requests,input_tokens,cache_creation_tokens,cache_read_tokens,output_tokens,total_tokens,actual_cost_usd,standard_cost_usd,favourite_model,last_used_at')
    expect(csv).toContain('"m,odel"')
    expect(csv).toContain('"key, ""quoted"""')
    expect(csv).toContain('"model\nwith newline"')
    expect(csv).not.toContain('account_cost')
    expect(csv).not.toContain('user_id')
    expect(csv).not.toContain('secret')
    expect(csv).toContain('2026-10-01')
    expect(csv).not.toContain('2026-11-01')
  })
})
