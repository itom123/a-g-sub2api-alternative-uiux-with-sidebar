import type { UserSubscription } from '@/types'
import { formatCny } from '@/utils/currency'

export type SubscriptionQuotaPeriod = 'daily' | 'weekly' | 'monthly'
export type SubscriptionQuotaState = 'finite' | 'unlimited' | 'unknown'

export interface SubscriptionQuotaWindow {
  period: SubscriptionQuotaPeriod
  oneTime: boolean
  state: SubscriptionQuotaState
  used: number | null
  limit: number | null
  remaining: number | null
  usagePercentage: number | null
}

export interface SubscriptionAllowance {
  id: number
  name: string | null
  groupId: number
  expiresAt: string | null
  state: SubscriptionQuotaState
  windows: SubscriptionQuotaWindow[]
  limitingWindow: SubscriptionQuotaWindow | null
  urgency: number
}

/** Values come from /subscriptions/active; never reconstruct or pool its windows. */
export function getSubscriptionAllowance(subscription: UserSubscription): SubscriptionAllowance {
  const windows = (['daily', 'weekly', 'monthly'] as const).map((period): SubscriptionQuotaWindow => {
    const rawLimit = subscription.group?.[`${period}_limit_usd`]
    const rawUsed = subscription[`${period}_usage_usd`]
    const used = typeof rawUsed === 'number' && Number.isFinite(rawUsed) && rawUsed >= 0 ? rawUsed : null
    const validLimit = rawLimit === null || (typeof rawLimit === 'number' && Number.isFinite(rawLimit))
    const state: SubscriptionQuotaState = !subscription.group || !validLimit || used === null
      ? 'unknown'
      : rawLimit === null || (typeof rawLimit === 'number' && rawLimit <= 0) ? 'unlimited' : 'finite'
    const limit = state === 'finite' ? rawLimit as number : null
    return {
      period,
      oneTime: period === 'daily' && isOneTimeDailyQuota(subscription),
      state,
      used,
      limit,
      remaining: limit !== null && used !== null ? Math.max(0, limit - used) : null,
      usagePercentage: limit !== null && used !== null ? used / limit * 100 : null
    }
  })
  const finiteWindows = windows.filter((window) => window.state === 'finite')
  const limitingWindow = finiteWindows.reduce<SubscriptionQuotaWindow | null>((smallest, window) => {
    return !smallest || window.remaining! < smallest.remaining! ? window : smallest
  }, null)
  return {
    id: subscription.id,
    name: subscription.group?.name?.trim() || null,
    groupId: subscription.group_id,
    expiresAt: subscription.expires_at,
    state: windows.some((window) => window.state === 'unknown') ? 'unknown' : limitingWindow ? 'finite' : 'unlimited',
    windows,
    limitingWindow,
    urgency: Math.max(0, ...finiteWindows.map((window) => window.usagePercentage!))
  }
}

/** Preserve the existing highest-usage urgency order, with deterministic identity ties. */
export function getSubscriptionAllowances(subscriptions: UserSubscription[]): SubscriptionAllowance[] {
  return subscriptions.map(getSubscriptionAllowance).sort((a, b) => b.urgency - a.urgency || a.id - b.id)
}

export function formatSubscriptionMoney(value: number): string {
  const converted = formatCny(value, 2)
  return converted === '-' ? `$${value.toFixed(2)}` : converted
}

const ONE_DAY_MS = 24 * 60 * 60 * 1000

export type ExpirationDateRelation = 'expired' | 'today' | 'tomorrow' | 'later'

export type RemainingExpiryDuration =
  | { unit: 'days'; days: number }
  | { unit: 'hoursMinutes'; hours: number; minutes: number }

export interface RemainingDurationParts {
  days: number
  hours: number
  minutes: number
}

export function isOneTimeDailyQuota(
  subscription: Pick<UserSubscription, 'starts_at' | 'expires_at'>
): boolean {
  if (!subscription.starts_at || !subscription.expires_at) return false

  const startsAt = new Date(subscription.starts_at).getTime()
  const expiresAt = new Date(subscription.expires_at).getTime()

  if (!Number.isFinite(startsAt) || !Number.isFinite(expiresAt)) return false

  return expiresAt <= startsAt + ONE_DAY_MS
}

export function getRemainingDurationParts(
  targetAt: Date | string,
  now: Date = new Date()
): RemainingDurationParts | null {
  const targetTime = targetAt instanceof Date ? targetAt.getTime() : new Date(targetAt).getTime()
  const nowTime = now.getTime()

  if (!Number.isFinite(targetTime) || !Number.isFinite(nowTime)) return null

  const diffMs = targetTime - nowTime
  if (diffMs <= 0) return null

  const totalMinutes = Math.floor(diffMs / (1000 * 60))
  const days = Math.floor(totalMinutes / (24 * 60))
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60)
  const minutes = totalMinutes % 60

  return { days, hours, minutes }
}

export function getExpirationDateRelation(
  targetAt: Date | string,
  now: Date = new Date()
): ExpirationDateRelation | null {
  const target = targetAt instanceof Date ? targetAt : new Date(targetAt)
  const targetTime = target.getTime()
  const nowTime = now.getTime()

  if (!Number.isFinite(targetTime) || !Number.isFinite(nowTime)) return null
  if (targetTime <= nowTime) return 'expired'

  const targetDay = Date.UTC(target.getFullYear(), target.getMonth(), target.getDate())
  const currentDay = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())
  const calendarDays = Math.round((targetDay - currentDay) / ONE_DAY_MS)

  if (calendarDays === 0) return 'today'
  if (calendarDays === 1) return 'tomorrow'
  return 'later'
}

export function getRemainingExpiryDuration(
  targetAt: Date | string,
  now: Date = new Date()
): RemainingExpiryDuration | null {
  const targetTime = targetAt instanceof Date ? targetAt.getTime() : new Date(targetAt).getTime()
  const nowTime = now.getTime()

  if (!Number.isFinite(targetTime) || !Number.isFinite(nowTime)) return null

  const diffMs = targetTime - nowTime
  if (diffMs <= 0) return null
  if (diffMs >= ONE_DAY_MS) {
    return { unit: 'days', days: Math.ceil(diffMs / ONE_DAY_MS) }
  }

  const totalMinutes = Math.ceil(diffMs / (60 * 1000))
  return {
    unit: 'hoursMinutes',
    hours: Math.floor(totalMinutes / 60),
    minutes: totalMinutes % 60
  }
}
