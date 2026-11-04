import { describe, expect, it } from 'vitest'
import { aggregateConsistency, aggregateRolling30d, aggregateRollingAndConsistency, deriveDashboardHealth } from '../aggregate'
import { trendPoint } from './testHelpers'
import type { UsageStatsResponse } from '@/types'

describe('dashboard aggregate snapshots', () => {
  it('keeps rolling totals authoritative and shared with consistency', () => {
    const trend = Array.from({ length: 7 }, (_, index) => trendPoint({ date: `2026-10-${25 + index}`, requests: index < 5 ? 6 : 5, total_tokens: 1500 }))
    const { rolling30, consistency } = aggregateRollingAndConsistency('2026-10-03', '2026-11-01', { total_requests: 40, total_tokens: 10500, total_actual_cost: 3 } as UsageStatsResponse, trend)
    expect(rolling30).toMatchObject({ requests: 40, actualCostUsd: 3, activeDays: 7, tokensPerCall: 263 })
    expect(consistency).toMatchObject({ requests: 40, activeDays30: 7, tokensPerCall: 263 })
    expect(consistency.callsPerActiveDay).toBeCloseTo(40 / 7)
  })
  it('preserves missing data, true zero and free actual usage', () => {
    expect(aggregateRolling30d('a', 'b', null, [])).toMatchObject({ requests: null, actualCostUsd: null })
    expect(aggregateRolling30d('a', 'b', { total_requests: 0, total_tokens: 0, total_actual_cost: 0 } as UsageStatsResponse, [])).toMatchObject({ requests: 0, actualCostUsd: 0, tokensPerCall: 0 })
    expect(aggregateRolling30d('a', 'b', null, [trendPoint({ actual_cost: 0 })]).actualCostUsd).toBe(0)
    expect(aggregateConsistency([trendPoint({ requests: 0, total_tokens: NaN })])).toMatchObject({ callsPerActiveDay: 0, tokensPerCall: 0 })
  })
  it('computes recent versus prior seven-day trend without cross-account comparisons', () => {
    const points = [...Array.from({ length: 7 }, () => trendPoint({ requests: 4 })), ...Array.from({ length: 7 }, () => trendPoint({ requests: 5 }))]
    expect(aggregateConsistency(points).trend7d).toBe('up')
    expect(aggregateConsistency(points.reverse()).trend7d).toBe('down')
    expect(aggregateConsistency([trendPoint({ requests: 2 })]).trend7d).toBe('steady')
  })
  it('does not equate unavailable snapshots with healthy zero activity', () => {
    expect(deriveDashboardHealth({ loaded: false })).toBe('awaiting_data')
    expect(deriveDashboardHealth({ loaded: true, stale: true })).toBe('attention')
    expect(deriveDashboardHealth({ loaded: true })).toBe('all_good')
  })
})
