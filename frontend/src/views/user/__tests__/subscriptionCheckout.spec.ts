import { describe, expect, it } from 'vitest'
import type { UserSubscription } from '@/types'
import type { PaymentSessionSummary, SubscriptionPlan } from '@/types/payment'
import { hasActivePlatformSubscription, isOpenSubscriptionBill, shouldRefreshSubscriptionBill } from '../subscriptionCheckout'

const plan = { group_id: 3, group_platform: 'openai' } as SubscriptionPlan
const now = Date.parse('2026-11-01T00:00:00Z')
const sub = (overrides = {}) => ({ group_id: 7, group: { platform: 'openai' }, status: 'active', expires_at: '2099-01-01T00:00:00Z', ...overrides }) as UserSubscription

describe('subscription checkout rules', () => {
  it('blocks same-platform and same-group active subscriptions', () => {
    expect(hasActivePlatformSubscription(plan, [sub()], now)).toBe(true)
    expect(hasActivePlatformSubscription(plan, [sub({ group_id: 3, group: null })], now)).toBe(true)
  })

  it('allows other platforms, expired and suspended subscriptions', () => {
    expect(hasActivePlatformSubscription(plan, [sub({ group: { platform: 'gemini' } })], now)).toBe(false)
    expect(hasActivePlatformSubscription(plan, [sub({ expires_at: '2020-01-01T00:00:00Z' })], now)).toBe(false)
    expect(hasActivePlatformSubscription(plan, [sub({ status: 'suspended' })], now)).toBe(false)
  })

  it('keeps an uncertain cancellation discoverable for retry without allowing a fresh bill', () => {
    expect(isOpenSubscriptionBill({ status: 'CANCELLING' } as PaymentSessionSummary)).toBe(true)
    expect(isOpenSubscriptionBill({ status: 'CANCELLED' } as PaymentSessionSummary)).toBe(false)
  })

  it('does not poll cancelled, fulfilled, or stale expired bills forever', () => {
    for (const status of ['CANCELLED', 'FULFILLED', 'REFUNDED']) {
      expect(shouldRefreshSubscriptionBill({ status } as PaymentSessionSummary, now)).toBe(false)
    }
    const bill = { status: 'PENDING', orders: [{ status: 'EXPIRED', expires_at: '2020-01-01T00:00:00Z' }] } as PaymentSessionSummary
    expect(shouldRefreshSubscriptionBill(bill, now)).toBe(false)
    expect(shouldRefreshSubscriptionBill({ ...bill, status: 'PAID' }, now)).toBe(true)
    expect(shouldRefreshSubscriptionBill({ ...bill, orders: [{ ...bill.orders![0], status: 'PENDING', expires_at: '2099-01-01T00:00:00Z' }] }, now)).toBe(true)
  })
})
