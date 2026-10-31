import { test, expect, type Page } from '@playwright/test'
import { prepare, user } from './fixtures'

const stats = {
  total_api_keys: 2, active_api_keys: 2, total_requests: 120, today_requests: 3,
  total_tokens: 50000, today_tokens: 7000, today_actual_cost: 2, total_actual_cost: 10,
  total_cost: 20, today_cost: 4,
  by_platform: [{ platform: 'openai', total_requests: 120, total_tokens: 50000,
    total_actual_cost: 10, today_actual_cost: 2, today_requests: 3, today_tokens: 7000 }],
}
const trend = [{ date: '2026-10-30', requests: 3, input_tokens: 500, output_tokens: 100,
  cache_read_tokens: 1000, cache_creation_tokens: 50, total_tokens: 1650, cost: 4, actual_cost: 2 }]
async function fixtures(page: Page, theme: 'light' | 'dark') {
  await prepare(page, theme)
  const apiRequests: string[] = []
  let balance = 20
  let fail = false
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
    apiRequests.push(path)
    let data: unknown
    if (path === '/currency') return route.fulfill({ json: { cnyPerUsd: 7, source: 'fixture', rateDate: '2026-10-31' } })
    if (path === '/user/profile') data = { ...user, role: 'user', balance, frozen_balance: 5 }
    else if (path === '/usage/dashboard/stats') data = stats
    else if (path === '/usage/dashboard/trend') data = { trend }
    else if (path === '/usage/dashboard/models') data = { models: [{ model: 'example-account-model', requests: 3, total_tokens: 1650, actual_cost: 2, cost: 4 }] }
    else if (path === '/user/platform-quotas') data = { platform_quotas: [{ platform: 'openai', daily_limit_usd: 10, daily_usage_usd: 2, weekly_limit_usd: null, monthly_limit_usd: null }] }
    else if (path === '/usage') data = { items: [{ id: 1, model: 'example-account-model', input_tokens: 500, output_tokens: 100, cache_read_tokens: 1000, cache_creation_tokens: 50, actual_cost: 2, total_cost: 4, created_at: '2026-10-30T12:00:00Z' }], total: 1 }
    else return route.fallback()
    if (fail && path.startsWith('/usage/')) return route.fulfill({ status: 503, json: { message: 'unavailable' } })
    return route.fulfill({ json: { code: 0, data } })
  })
  return { apiRequests, setBalance: (value: number) => { balance = value }, fail: () => { fail = true } }
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme} account balance, charts and calls are usable without admin access`, async ({ page }, info) => {
    const fixture = await fixtures(page, theme)
    await page.goto('/dashboard', { waitUntil: 'networkidle' })
    await expect(page.getByTestId('account-balance')).toHaveText('¥140.00')
    await expect(page.getByRole('meter')).toHaveAttribute('value', '20')
    await expect(page.getByTestId('account-spending-trend').locator('canvas')).toBeVisible()
    await expect(page.getByText('1,650 tokens', { exact: true })).toBeVisible()
    await expect(page.getByRole('checkbox', { name: 'Refresh every 30 seconds' })).toBeChecked()
    expect(fixture.apiRequests.filter(path => path.startsWith('/admin/'))).toEqual([])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
    await page.screenshot({ path: info.outputPath(`account-usage-${theme}.png`), fullPage: true })
    if (info.project.name === 'phone') await page.getByRole('button', { name: 'Toggle menu', exact: true }).click()
    await expect(page.locator('.sidebar-nav').getByRole('link', { name: 'Usage & balance', exact: true })).toBeVisible()
  })
}

test('manual refresh updates the current balance and preserves it on failure', async ({ page }) => {
  const fixture = await fixtures(page, 'light')
  await page.goto('/dashboard', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('account-balance')).toHaveText('¥140.00')
  fixture.setBalance(19)
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByTestId('account-balance')).toHaveText('¥133.00')
  await expect(page.locator('.ag-app-header')).toContainText('¥133.00')
  fixture.fail()
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Refresh failed')
  await expect(page.getByTestId('account-balance')).toHaveText('¥133.00')
})

test('dashboard requires a signed-in account', async ({ page }) => {
  await prepare(page, 'light', false)
  await page.goto('/dashboard', { waitUntil: 'networkidle' })
  await expect(page).toHaveURL(/\/login/)
  await expect(page.getByTestId('account-usage-page')).toHaveCount(0)
})
