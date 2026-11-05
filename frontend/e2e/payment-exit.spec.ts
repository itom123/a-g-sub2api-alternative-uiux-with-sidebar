import { test, expect, type Page } from '@playwright/test'
import { prepare } from './fixtures'

const plan = { id: 7, group_id: 17, group_platform: 'openai', name: 'Plan A · Regression fixture', description: 'Full subscription payment', price: 195.18, fixed_price_cny: 1566, validity_days: 30, validity_unit: 'days', features: [], for_sale: true, sort_order: 0 }
const legacy = { id: 42, plan_id: 7, plan_name: plan.name, subscription_group_id: 17, target_amount: 1566, paid_amount: 0, currency: 'CNY', status: 'PENDING', created_at: '2026-10-20T12:00:00Z', orders: [{ id: 123, pay_amount: 1000, status: 'EXPIRED', expires_at: '2026-10-20T12:30:00Z', provider_closed: false, out_trade_no: 'fixture-expired-order' }] }

async function paymentHarness(page: Page, expired = false) {
  await prepare(page, 'dark', true, 'user', 'zh')
  let cancelled = false
  let cancelling = false
  const expiresAt = new Date(Date.now() + 30 * 60_000).toISOString()
  let orderCreated = false
  const bodies: Record<string, unknown>[] = []
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/payment/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname.replace('/api/v1/payment', '')
    let data: unknown = {}
    if (path === '/access') data = { allowed: true }
    if (path === '/checkout-info') data = { methods: { wxpay: { currency: 'CNY', single_min: 0, single_max: 0, available: true, fee_rate: 0 } }, global_min: 0, global_max: 0, plans: [plan], balance_disabled: false, subscription_allowed: true, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 7.15, recharge_fee_rate: 0, help_text: '', help_image_url: '', stripe_publishable_key: '' }
    if (path === '/sessions/my') data = expired ? [{ ...legacy, status: cancelled ? 'CANCELLED' : cancelling ? 'CANCELLING' : 'PENDING' }] : []
    if (path === '/sessions') data = { items: expired ? [{ ...legacy, status: cancelled ? 'CANCELLED' : cancelling ? 'CANCELLING' : 'PENDING' }] : [], total: expired ? 1 : 0 }
    if (path === '/subscription-checkout') {
      const session = new URL(request.url()).searchParams.get('session_id')
      data = { total_cents: 156600, next_amount: 1566, ...(expired && session && !cancelled ? { pending_order: { id: 123, checking: true } } : {}) }
    }
    if (path === '/sessions/42/cancel') { cancelled = true; data = { cancelled: true } }
    if (path === '/orders' && request.method() === 'POST') {
      orderCreated = true
      bodies.push(request.postDataJSON())
      data = { order_id: 124, amount: 195.18, pay_amount: 1566, fee_rate: 0, payment_type: 'wxpay', qr_code: 'weixin://fixture/124', out_trade_no: 'fixture-full-order', expires_at: expiresAt }
    }
    if (path === '/orders/124') data = { id: 124, status: cancelled ? 'CANCELLED' : cancelling ? 'CANCELLING' : 'PENDING', order_type: 'subscription', pay_amount: 1566, out_trade_no: 'fixture-full-order', expires_at: expiresAt }
    if (path === '/orders/124/cancel') { cancelled = true; data = { message: 'cancelled' } }
    await route.fulfill({ json: { code: 0, data } })
  })
  return { bodies, errors, wasCreated: () => orderCreated, markCancelling: () => { cancelling = true } }
}

test('expired legacy waiting can be dismissed, cancelled, and never traps navigation', async ({ page }, testInfo) => {
  const harness = await paymentHarness(page, true)
  await page.goto('/purchase?tab=subscription&group=17')
  await expect(page.getByRole('button', { name: '核对支付状态', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '取消订单', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '返回套餐列表', exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('expired-bill-with-exit.png'), fullPage: true })
  await page.getByRole('button', { name: '返回套餐列表', exact: true }).click()
  await expect(page.getByRole('button', { name: '立即开通', exact: true })).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: '取消订单', exact: true }).click()
  await expect(page.getByRole('button', { name: '立即开通', exact: true })).toBeVisible()
  await page.getByTestId('exit-checkout').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await page.goto('/purchase')
  await expect(page.getByText('充值账户', { exact: true })).toBeVisible()
  expect(harness.wasCreated()).toBe(false)
  expect(harness.errors).toEqual([])
})

test('an active same-platform subscription blocks another purchase without blocking navigation', async ({ page }) => {
  const harness = await paymentHarness(page)
  await page.route('**/api/v1/subscriptions/active**', route => route.fulfill({ json: { code: 0, data: [{ id: 9, user_id: 42, group_id: 99, group: { platform: 'openai', name: 'Existing plan' }, status: 'active', expires_at: '2099-01-01T00:00:00Z' }] } }))
  await page.goto('/subscription')
  await expect(page.getByRole('button', { name: /^支付.*1,566/ })).toBeDisabled()
  await expect(page.getByText('您已拥有该平台的有效订阅。', { exact: true })).toBeVisible()
  await page.getByTestId('exit-checkout').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  expect(harness.wasCreated()).toBe(false)
  expect(harness.errors).toEqual([])
})

test('gateway cancellation failure never blocks leaving the expired bill', async ({ page }) => {
  const harness = await paymentHarness(page, true)
  await page.route('**/api/v1/payment/sessions/42/cancel', route => {
    harness.markCancelling()
    return route.fulfill({ status: 409, json: { code: 409, reason: 'PAYMENT_CHECKING', message: 'Gateway closure unconfirmed' } })
  })
  await page.goto('/purchase?tab=subscription&group=17')
  await page.getByRole('button', { name: '取消订单', exact: true }).click()
  await expect(page.getByRole('button', { name: '返回套餐列表', exact: true })).toBeEnabled()
  await page.getByTestId('exit-checkout').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await page.goto('/purchase?tab=subscription&group=17')
  await expect(page.getByTestId('cancel-subscription-bill')).toBeEnabled()
  await expect(page.getByText('正在确认网关订单关闭，尚未完成取消。您可以重试取消，或先返回正常页面；请勿重复付款。', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /^支付.*1,566/ })).toHaveCount(0)
})

test('one-time subscription order can be cancelled without a second confirmation', async ({ page }) => {
  const harness = await paymentHarness(page)
  await page.goto('/purchase?tab=subscription&group=17')
  await page.getByRole('button', { name: /^支付.*1,566/ }).click()
  await expect(page.getByRole('button', { name: '取消订单', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '取消订单', exact: true }).click()
  await expect(page.getByRole('button', { name: '立即开通', exact: true })).toBeVisible()
  expect(harness.bodies).toHaveLength(1)
  expect(harness.errors).toEqual([])
})

test('new subscription pays 1566 once and can dismiss or cancel waiting', async ({ page }, testInfo) => {
  const harness = await paymentHarness(page)
  await page.goto('/purchase?tab=subscription&group=17')
  await expect(page.getByText('订阅一次性全额支付，不再使用分次支付。', { exact: true })).toBeVisible()
  await expect(page.locator('#subscription-installment')).toHaveCount(0)
  await page.getByRole('button', { name: /^支付.*1,566/ }).click()
  await expect(page.getByRole('button', { name: '关闭等待提示', exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('one-time-payment-with-exit.png'), fullPage: true })
  await page.getByRole('button', { name: '关闭等待提示', exact: true }).click()
  await expect(page.getByRole('button', { name: '立即开通', exact: true })).toBeVisible()
  expect(harness.bodies).toHaveLength(1)
  expect(harness.bodies[0]).toMatchObject({ amount: 1566, expected_total_cents: 156600, order_type: 'subscription', plan_id: 7 })
  expect(harness.bodies[0]).not.toHaveProperty('payment_session_id')
  expect(harness.errors).toEqual([])
})
