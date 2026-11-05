import type { UserSubscription } from '@/types'
import type { PaymentSessionSummary, SubscriptionPlan } from '@/types/payment'

export function hasActivePlatformSubscription(plan: SubscriptionPlan, subscriptions: UserSubscription[], now = Date.now()): boolean {
  return subscriptions.some(sub => sub.status === 'active'
    && (!sub.expires_at || Date.parse(sub.expires_at) > now)
    && (sub.group_id === plan.group_id || (!!plan.group_platform && sub.group?.platform === plan.group_platform)))
}

export function isOpenSubscriptionBill(bill: PaymentSessionSummary): boolean {
  return ['PENDING', 'CANCELLING', 'PAID', 'ALLOCATION_FAILED'].includes(bill.status)
}

export function shouldRefreshSubscriptionBill(bill: PaymentSessionSummary, now = Date.now()): boolean {
  if (!isOpenSubscriptionBill(bill)) return false
  if (bill.status === 'PAID' || bill.status === 'ALLOCATION_FAILED') return true
  return bill.orders?.some(order => ['PAID', 'RECHARGING'].includes(order.status)
    || (order.status === 'PENDING' && !order.provider_closed && Date.parse(order.expires_at) > now)) ?? false
}
