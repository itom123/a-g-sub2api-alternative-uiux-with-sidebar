import { afterEach, describe, expect, it } from 'vitest'
import type { Group, UserSubscription } from '@/types'
import { currencyRate } from '@/utils/currency'

import { formatSubscriptionMoney, getSubscriptionAllowance, getSubscriptionAllowances, getExpirationDateRelation, getRemainingExpiryDuration } from '../subscriptionQuota'

function subscription(over: Partial<UserSubscription> = {}): UserSubscription {
  return {
    id: 1, user_id: 42, group_id: 7, status: 'active', starts_at: '2026-11-01T00:00:00Z', expires_at: '2026-12-01T00:00:00Z',
    daily_usage_usd: 9, weekly_usage_usd: 70, monthly_usage_usd: 20,
    daily_window_start: null, weekly_window_start: null, monthly_window_start: null, created_at: '', updated_at: '',
    group: { name: 'Plan', daily_limit_usd: 10, weekly_limit_usd: 100, monthly_limit_usd: 25 } as Group,
    ...over
  }
}

afterEach(() => { currencyRate.value = null })

describe('per-plan subscription allowances', () => {
  it('selects the smallest remaining dollar window, not the highest percentage', () => {
    const result = getSubscriptionAllowance(subscription({ daily_usage_usd: 9, weekly_usage_usd: 95, monthly_usage_usd: 20 }))
    expect(result.limitingWindow).toMatchObject({ period: 'daily', remaining: 1 })
    expect(result.windows.map((window) => window.remaining)).toEqual([1, 5, 5])
  })

  it('preserves exact zero and clamps overuse without changing reported used amounts', () => {
    const result = getSubscriptionAllowance(subscription({ daily_usage_usd: 10, weekly_usage_usd: 120 }))
    expect(result.windows[0].remaining).toBe(0)
    expect(result.windows[1]).toMatchObject({ remaining: 0, used: 120, limit: 100 })
    expect(result.limitingWindow?.period).toBe('daily')
  })

  it('treats only explicit null and finite nonpositive limits as uncapped', () => {
    const result = getSubscriptionAllowance(subscription({ group: { name: 'Uncapped', daily_limit_usd: 0, weekly_limit_usd: -1, monthly_limit_usd: null } as Group }))
    expect(result.state).toBe('unlimited')
    expect(result.limitingWindow).toBeNull()
    expect(result.windows.every((window) => window.state === 'unlimited')).toBe(true)
  })

  it('does not let uncapped windows override a finite allowance', () => {
    const result = getSubscriptionAllowance(subscription({ group: { daily_limit_usd: 10, weekly_limit_usd: null, monthly_limit_usd: 0 } as Group }))
    expect(result.state).toBe('finite')
    expect(result.limitingWindow).toMatchObject({ period: 'daily', remaining: 1 })
  })

  it.each([undefined, NaN, Infinity, '10'])('represents invalid limit %s as unknown', (value) => {
    const result = getSubscriptionAllowance(subscription({ group: { daily_limit_usd: value, weekly_limit_usd: null, monthly_limit_usd: null } as unknown as Group }))
    expect(result.state).toBe('unknown')
    expect(result.windows[0].state).toBe('unknown')
  })

  it.each([undefined, NaN, Infinity, -1, '0'])('does not fabricate zero from invalid usage %s', (value) => {
    const result = getSubscriptionAllowance(subscription({ daily_usage_usd: value as number }))
    expect(result.windows[0]).toMatchObject({ state: 'unknown', used: null, remaining: null })
  })

  it('keeps a missing group unknown, never unlimited', () => {
    expect(getSubscriptionAllowance(subscription({ group: undefined })).state).toBe('unknown')
  })

  it('keeps plans separate and sorts usage urgency with an ID tie-break', () => {
    const results = getSubscriptionAllowances([subscription({ id: 9 }), subscription({ id: 2 }), subscription({ id: 1, daily_usage_usd: 10 })])
    expect(results.map((plan) => plan.id)).toEqual([1, 2, 9])
    expect(results.map((plan) => plan.limitingWindow?.remaining)).toEqual([0, 1, 1])
  })

  it('marks a one-day subscription as a one-time daily allowance', () => {
    const result = getSubscriptionAllowance(subscription({ expires_at: '2026-11-02T00:00:00Z' }))
    expect(result.windows[0].oneTime).toBe(true)
    expect(result.windows[1].oneTime).toBe(false)
  })

  it('uses two decimal places, including zero, and an honest USD fallback', () => {
    currencyRate.value = null
    expect(formatSubscriptionMoney(0)).toBe('$0.00')
    expect(formatSubscriptionMoney(0.04)).toBe('$0.04')
    currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
    expect(formatSubscriptionMoney(0.04)).toBe('¥0.28')
    expect(formatSubscriptionMoney(0)).toBe('¥0.00')
  })
})

describe('subscription expiry timing', () => {
  it('uses local calendar dates for today and tomorrow', () => {
    const now = new Date(2026, 2, 7, 23, 30)

    expect(getExpirationDateRelation(new Date(2026, 2, 7, 23, 45), now)).toBe('today')
    expect(getExpirationDateRelation(new Date(2026, 2, 8, 3, 30), now)).toBe('tomorrow')
  })

  it('treats the exact expiry instant and elapsed expiries as expired', () => {
    const now = new Date(2026, 6, 30, 9, 0)

    expect(getExpirationDateRelation(now, now)).toBe('expired')
    expect(getRemainingExpiryDuration(now, now)).toBeNull()
    expect(getExpirationDateRelation(new Date(2026, 6, 30, 8, 59), now)).toBe('expired')
    expect(getRemainingExpiryDuration(new Date(2026, 6, 30, 8, 59), now)).toBeNull()
  })

  it('rejects invalid target and current dates', () => {
    const invalid = new Date('invalid')
    const valid = new Date(2026, 6, 30, 9, 0)

    expect(getExpirationDateRelation(invalid, valid)).toBeNull()
    expect(getExpirationDateRelation(valid, invalid)).toBeNull()
    expect(getRemainingExpiryDuration(invalid, valid)).toBeNull()
    expect(getRemainingExpiryDuration(valid, invalid)).toBeNull()
  })

  it('returns rounded-up hours and minutes for an expiry under 24 hours away', () => {
    const now = new Date(2026, 6, 30, 9, 0)

    expect(getRemainingExpiryDuration(new Date(2026, 6, 31, 8, 30), now)).toEqual({
      unit: 'hoursMinutes',
      hours: 23,
      minutes: 30
    })
    expect(getRemainingExpiryDuration(new Date(now.getTime() + 1), now)).toEqual({
      unit: 'hoursMinutes',
      hours: 0,
      minutes: 1
    })
    expect(getRemainingExpiryDuration(new Date(now.getTime() + 23 * 60 * 60 * 1000 + 1), now)).toEqual({
      unit: 'hoursMinutes',
      hours: 23,
      minutes: 1
    })
  })

  it('preserves rounded-up day display from 24 hours onward', () => {
    const now = new Date(2026, 6, 30, 9, 0)

    expect(getRemainingExpiryDuration(new Date(now.getTime() + 24 * 60 * 60 * 1000), now)).toEqual({
      unit: 'days',
      days: 1
    })
    expect(getRemainingExpiryDuration(new Date(now.getTime() + 24 * 60 * 60 * 1000 + 1), now)).toEqual({
      unit: 'days',
      days: 2
    })
  })
})
