import { describe, expect, it } from 'vitest'
import { aggregateActiveNow, aggregateConsistency, aggregateKeyUsage, aggregateRolling30d, aggregateRollingAndConsistency, deriveDashboardHealth } from '../aggregate'
import { usage, trendPoint } from './testHelpers'
import type { UsageStatsResponse } from '@/types'

describe('dashboard insight aggregation', () => {
  it('aggregates bounded usage rows into key rows using actual cost and token buckets', () => {
    const rows = [
      usage({ id: 1, api_key_id: 11, model: 'z-model', input_tokens: 2, cache_creation_tokens: 3, cache_read_tokens: 4, output_tokens: 5, actual_cost: 0.7 }),
      usage({ id: 2, api_key_id: 11, model: 'a-model', input_tokens: 1, output_tokens: 1, actual_cost: 0.2, created_at: '2026-10-31T13:00:00.000Z' }),
      usage({ id: 3, api_key_id: 12, model: 'other', actual_cost: 0.4 }),
    ]
    const result = aggregateKeyUsage(rows, [{ id: 11, name: 'one', status: 'active', last_used_at: null }, { id: 12, name: 'two', status: 'inactive', last_used_at: null }], { '11': { api_key_id: 11, total_actual_cost: 9, today_actual_cost: 2 } })
    expect(result[0]).toMatchObject({ apiKeyId: 11, lifetimeCostUsd: 9, todayCostUsd: 2, requests: 2, inputTokens: 3, cacheCreationTokens: 3, cacheReadTokens: 4, outputTokens: 6, totalTokens: 16, favouriteModel: 'z-model' })
    expect(result[0].selectedRangeCostUsd).toBeCloseTo(0.9)
    expect(result[1].selectedRangeCostUsd).toBe(0.4)
  })

  it('breaks favourite-model ties deterministically by token count and then name', () => {
    const rows = [usage({ id: 1, model: 'z', input_tokens: 1, output_tokens: 0 }), usage({ id: 2, model: 'a', input_tokens: 1, output_tokens: 0 })]
    expect(aggregateKeyUsage(rows, [{ id: 11, name: 'key', status: 'active', last_used_at: null }])[0].favouriteModel).toBe('a')
    const tokensTieBroken = aggregateKeyUsage([usage({ id: 3, model: 'z', input_tokens: 100 }), usage({ id: 4, model: 'a', input_tokens: 1 })], [{ id: 11, name: 'key', status: 'active', last_used_at: null }])
    expect(tokensTieBroken[0].favouriteModel).toBe('z')
  })

  it('marks a scan capped only when more pages are known to exist', () => {
    const key = [{ id: 11, name: 'key', status: 'active', last_used_at: null }]
    expect(aggregateKeyUsage([], key, {}, { pagesFetched: 5, maxPages: 5, hasMore: false })[0].truncated).toBe(false)
    expect(aggregateKeyUsage([], key, {}, { pagesFetched: 5, maxPages: 5, hasMore: true })[0].truncated).toBe(true)
    expect(aggregateKeyUsage([], key, {}, { pagesFetched: 5, maxPages: 5 })[0].truncated).toBe(true)
  })

  it('builds separate exact active-now windows with model shares and cap metadata', () => {
    const now = Date.parse('2026-10-31T13:00:00.000Z')
    const rows = [
      usage({ id: 1, model: 'a', created_at: '2026-10-31T12:30:00.000Z', input_tokens: 10, output_tokens: 0 }),
      usage({ id: 2, model: 'a', created_at: '2026-10-31T02:00:00.000Z' }),
      usage({ id: 3, model: 'b', created_at: '2026-10-30T14:30:00.000Z' }),
    ]
    const windows = aggregateActiveNow(rows, { now, pagesFetched: 5, maxPages: 5, hasMore: false })
    expect(windows.map((window) => window.id)).toEqual(['60m', '12h', '24h'])
    expect(windows[0]).toMatchObject({ requests: 1, complete: true, cappedPages: null })
    expect(windows[1].requests).toBe(2)
    expect(windows[2].requests).toBe(3)
    expect(windows[2].modelShares[0]).toMatchObject({ model: 'a', requests: 2, share: 2 / 3 })
    const capped = aggregateActiveNow(rows, { now, pagesFetched: 5, maxPages: 5, hasMore: true })
    expect(capped.every((window) => !window.complete && window.cappedPages === 5)).toBe(true)
  })

  it('reports four calls in 60 minutes as a fractional RPM instead of zero', () => {
    const now = Date.parse('2026-10-31T13:00:00.000Z')
    const rows = Array.from({ length: 4 }, (_, index) => usage({ id: index + 1, created_at: `2026-10-31T12:${String(10 + index).padStart(2, '0')}:00.000Z` }))
    const window = aggregateActiveNow(rows, { now }).find((item) => item.id === '60m')
    expect(window).toMatchObject({ requests: 4, rpm: 4 / 60 })
    expect(window?.rpm).not.toBe(0)
  })

  it('derives consistency, including the 1.25 and 0.8 thresholds, without team comparisons', () => {
    const rising = aggregateConsistency([
      ...Array.from({ length: 7 }, (_, i) => trendPoint({ date: `2026-10-${10 + i}`, requests: 4 })),
      ...Array.from({ length: 7 }, (_, i) => trendPoint({ date: `2026-10-${17 + i}`, requests: 5 })),
    ])
    expect(rising.trend7d).toBe('up')
    const falling = aggregateConsistency([
      ...Array.from({ length: 7 }, (_, i) => trendPoint({ date: `2026-10-${10 + i}`, requests: 10 })),
      ...Array.from({ length: 7 }, (_, i) => trendPoint({ date: `2026-10-${17 + i}`, requests: 8 })),
    ])
    expect(falling.trend7d).toBe('down')
    const steady = aggregateConsistency([trendPoint({ requests: 2 }), trendPoint({ date: '2026-11-01', requests: 2 })])
    expect(steady.trend7d).toBe('steady')
    expect(JSON.stringify(steady)).not.toMatch(/team|peer|average/i)
  })

  it('handles zero and invalid numbers without NaN or Infinity', () => {
    const result = aggregateConsistency([trendPoint({ requests: 0, total_tokens: Number.NaN })])
    expect(result).toMatchObject({ activeDays30: 0, callsPerActiveDay: 0, tokensPerCall: 0 })
    expect(JSON.stringify(result)).not.toMatch(/NaN|Infinity/)
    const key = aggregateKeyUsage([usage({ actual_cost: Number.NaN, input_tokens: Number.NaN })], [{ id: 11, name: 'key', status: 'active', last_used_at: null }])[0]
    expect(key.selectedRangeCostUsd).toBe(0)
    expect(key.inputTokens).toBe(0)
  })

  it('keeps rolling 30-day summary independent of the selected range', () => {
    const stats = { total_requests: 10, total_tokens: 100, total_actual_cost: 3 } as UsageStatsResponse
    expect(aggregateRolling30d('2026-10-02', '2026-10-31', stats, [trendPoint({ requests: 3 }), trendPoint({ date: '2026-10-30', requests: 0 })])).toEqual({ startDate: '2026-10-02', endDate: '2026-10-31', actualCostUsd: 3, requests: 10, totalTokens: 100, activeDays: 1, tokensPerCall: 10 })
    expect(aggregateRolling30d('2026-10-02', '2026-10-31', null, [])).toMatchObject({ actualCostUsd: null, requests: null, totalTokens: null, activeDays: null, tokensPerCall: null })
    const fromTrend = aggregateRolling30d('2026-10-02', '2026-10-31', null, [trendPoint({ requests: 3, total_tokens: 9, actual_cost: 0.5 }), trendPoint({ date: '2026-10-30', requests: 0, total_tokens: 0, actual_cost: 0 })])
    expect(fromTrend).toMatchObject({ actualCostUsd: 0.5, requests: 3, totalTokens: 9, activeDays: 1, tokensPerCall: 3 })
  })

  it('uses the same rolling source for consistency and rounds shared metrics', () => {
    const trend = [
      trendPoint({ date: '2026-10-25', requests: 6, total_tokens: 1575 }),
      trendPoint({ date: '2026-10-26', requests: 6, total_tokens: 1575 }),
      trendPoint({ date: '2026-10-27', requests: 6, total_tokens: 1575 }),
      trendPoint({ date: '2026-10-28', requests: 6, total_tokens: 1575 }),
      trendPoint({ date: '2026-10-29', requests: 6, total_tokens: 1575 }),
      trendPoint({ date: '2026-10-30', requests: 5, total_tokens: 1312.5 }),
      trendPoint({ date: '2026-10-31', requests: 5, total_tokens: 1312.5 }),
    ]
    const { rolling30, consistency } = aggregateRollingAndConsistency('2026-10-02', '2026-10-31', { total_requests: 40, total_tokens: 10500, total_actual_cost: 3 } as UsageStatsResponse, trend)
    expect(consistency.requests).toBe(40)
    expect(consistency.activeDays30).toBe(7)
    expect(consistency.callsPerActiveDay).toBeCloseTo(40 / 7)
    expect(consistency.tokensPerCall).toBe(263)
    expect(rolling30.requests).toBe(40)
    expect(rolling30.activeDays).toBe(7)
    expect(Math.round(Number(rolling30.tokensPerCall))).toBe(consistency.tokensPerCall)
  })

  it('derives awaiting, attention, and all-good health', () => {
    expect(deriveDashboardHealth({ loaded: false })).toBe('awaiting_data')
    expect(deriveDashboardHealth({ loaded: true })).toBe('all_good')
    expect(deriveDashboardHealth({ loaded: true, truncated: true })).toBe('attention')
    expect(deriveDashboardHealth({ loaded: true, lowBalance: true })).toBe('attention')
  })
})
