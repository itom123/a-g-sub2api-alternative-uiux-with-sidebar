import { test, expect, type Page } from '@playwright/test'
import { prepare, settings } from './fixtures'

const instance = { id: 7, tenant_slug: 'fixture-workspace', name: 'Example workspace', status: 'running', created_at: '2026-10-01T09:00:00Z', updated_at: '2026-10-01T09:00:00Z' }
const models = ['example-model-with-a-very-long-identifier-'.repeat(3), 'reasoning-model', 'coding-model', 'general-model']
const fleet = { ...instance, user_id: 1, user_email: 'long-workspace-owner@example.test', billing_group: 'OpenClaw', billing_rate: .3, api_key_status: 'active', connection_count: 2, connected_count: 1, usage_cost: 1.5, usage_count: 999, allowed_models: models, default_model: models[0], mapping_enabled: true }
const fulfill = (data: unknown) => ({ json: { code: 0, data } })
async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
}
for (const theme of ['light', 'dark'] as const) {
  for (const width of [360, 390, 768, 1024, 1440, 1920]) {
    test(`${theme} responsive career and fleet ${width}px`, async ({ page }, info) => {
      test.skip(info.project.name === 'phone' && width !== 390, 'Desktop project covers the full viewport matrix.')
      const errors: string[] = []
      page.on('pageerror', error => errors.push(error.message))
      await page.setViewportSize({ width, height: 1000 })
      await prepare(page, theme, true, 'admin')
      await page.route('**/api/v1/admin/openclaw/instances*', route => route.fulfill(fulfill({ items: [fleet, { ...fleet, id: 8, name: 'Second workspace' }] })))
      await page.goto('/career-concierge', { waitUntil: 'networkidle' })
      await expect(page.getByRole('heading', { name: 'Personalised job advice' })).toBeVisible()
      await noOverflow(page)
      expect((await page.locator('.career-concierge-page').boundingBox())!.width).toBeGreaterThan((await page.locator('.ag-workspace').boundingBox())!.width - 70)
      await page.screenshot({ path: info.outputPath(`career-${width}.png`), fullPage: true })
      await page.goto('/admin/openclaw', { waitUntil: 'networkidle' })
      await expect(page.getByText('Example workspace', { exact: true })).toBeVisible()
      await expect(page.locator('.fleet-models li').first()).toHaveText(models[0])
      await noOverflow(page)
      const rootWidth = (await page.locator('.fleet-page').boundingBox())!.width
      expect(await page.locator('.fleet-table tbody tr').first().evaluate(el => getComputedStyle(el).display)).toBe(rootWidth >= 1200 ? 'table-row' : 'grid')
      await page.screenshot({ path: info.outputPath(`fleet-${width}.png`), fullPage: true })
      if (width >= 1024) {
        await page.locator('.sidebar-logo').click()
        await expect.poll(async () => (await page.locator('.fleet-page').boundingBox())!.width).toBeGreaterThan(rootWidth + 200)
        await noOverflow(page)
      }
      expect(errors).toEqual([])
    })
  }
}
const coverage = { requested_start: '2026-10-18T12:00:00Z', requested_end: '2026-10-19T12:00:00Z', coverage_start: '2026-10-18T12:00:00Z', data_through: '2026-10-19T12:00:00Z', computed_at: '2026-10-19T12:00:00Z', aggregation_lag_seconds: 0, coverage_complete: true, bucket_seconds: 3600 }
const baseMetric = { has_observation: false, request_count: 0, success_requests: 0, error_requests: 0, token_count: 0, rpm: 0, tpm: 0, error_rate: 0, cache_rate: 0, cache_rate_numerator: 0, cache_rate_denominator: 0, ttft: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null }, duration: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null } }
const health = { overall: 'unknown', error_rate: 'unknown', ttft: 'unknown', minimum_sample: 50 }
const rows = [
  { platform: 'openai', model: 'observed-low-sample', metrics: { ...baseMetric, has_observation: true }, health },
  { platform: 'openai', model: 'observed-errors-only', metrics: { ...baseMetric, has_observation: true, error_rate: 1 }, health },
  { platform: 'openai', model: 'unobserved-model', metrics: baseMetric, health },
]
async function monitorFixtures(page: Page) {
  await prepare(page, 'light')
  await page.addInitScript(() => { if (window.__APP_CONFIG__) window.__APP_CONFIG__.compact_home_enabled = false })
  await page.route('**/api/v1/settings/public*', route => route.fulfill(fulfill({ ...settings, compact_home_enabled: false })))
  await page.route('**/api/v1/channel-monitor-v2/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/models')) return route.fulfill(fulfill({ items: rows, coverage }))
    if (path.endsWith('/matrix')) return route.fulfill(fulfill({ items: rows.map(row => ({ ...row, group_id: 1, group_name: 'Standard', buckets: [{ bucket_start: '2026-10-19T11:00:00Z', metrics: row.metrics, health }] })), coverage, group_by: 'platform_group' }))
    if (path.endsWith('/dimensions')) return route.fulfill(fulfill({ platforms: [], groups: [], models: [] }))
    return route.fulfill(fulfill({ config: { refresh_interval_seconds: 60 }, coverage, metrics: rows[0].metrics, health, trend: [] }))
  })
  await page.route('**/api/v1/model-plaza*', route => route.fulfill(fulfill({ groups: [{ id: 1, name: 'Standard', platform: 'openai', models: rows.map(row => ({ name: row.model, platform: row.platform })) }] })))
  await page.route('**/api/v1/channels/available*', route => route.fulfill(fulfill([{ name: 'Standard', platforms: [{ platform: 'openai', groups: [{ id: 1 }] }] }])))
}
test('home and monitor preserve redacted and error-only traffic', async ({ page }) => {
  await monitorFixtures(page)
  await page.goto('/home', { waitUntil: 'networkidle' })
  await expect(page.locator('.ag-live-groups')).toContainText('observed-low-sample')
  await expect(page.locator('.ag-live-groups')).toContainText('observed-errors-only')
  await expect(page.locator('.ag-live-groups')).not.toContainText('unobserved-model')
  await page.getByRole('checkbox', { name: /显示待观测模型/ }).check()
  await expect(page.locator('.ag-live-groups')).toContainText('unobserved-model')
  await page.goto('/monitor', { waitUntil: 'networkidle' })
  await expect(page.locator('.ag-monitor-models tbody tr')).toHaveCount(2)
  await page.getByRole('checkbox', { name: /Show models awaiting observation/ }).check()
  await expect(page.locator('.ag-monitor-models tbody tr')).toHaveCount(3)
  const emptyRow = page.locator('.ag-monitor-models tbody tr').filter({ hasText: 'unobserved-model' })
  await expect(emptyRow.locator('td').nth(1)).toContainText('—')
  await expect(emptyRow).not.toContainText('100.0%')
  await noOverflow(page)
})
test('home distinguishes channel errors from empty permissions', async ({ page }) => {
  await monitorFixtures(page)
  await page.route('**/api/v1/channels/available*', route => route.fulfill({ status: 503, json: { message: 'Fixture unavailable' } }))
  await page.goto('/home', { waitUntil: 'networkidle' })
  await expect(page.getByRole('alert')).toContainText('可用分组刷新失败')
  await expect(page.getByText('暂无可用模型，请联系管理员确认分组权限。')).toHaveCount(0)
})
for (const lastUsed of [undefined, '2026-10-18T15:42:00Z']) {
  test(`connection activity timestamp ${lastUsed || 'absent'}`, async ({ page }) => {
    await prepare(page, 'light')
    await page.route('**/api/v1/openclaw/lifecycle*', route => route.fulfill(fulfill({ instance, can_create: false, self_service_enabled: true })))
    await page.route('**/api/v1/openclaw/instances/7/connections*', route => route.fulfill(fulfill({ items: [] })))
    await page.route('**/api/v1/openclaw/dashboard*', route => route.fulfill(fulfill({ instance, fleet_state: 'running', checked_at: '2026-10-19T12:00:00Z', connection_count: 0, connected_count: 0, usage_count: 999, last_used_at: lastUsed, api_key: { name: 'Billing key', status: 'active', usage_count: 999, usage_cost: 1, last_used_at: '2026-10-19T12:00:00Z' } })))
    await page.goto('/connections', { waitUntil: 'networkidle' })
    const activity = page.locator('.metric-card').filter({ hasText: 'Latest connection activity' })
    if (lastUsed) await expect(activity.locator('time')).toHaveAttribute('datetime', lastUsed)
    else await expect(activity).toContainText('No activity recorded')
    await expect(activity).not.toContainText('999')
    await expect(page.locator('.billing-api-stats')).toContainText('999')
    await noOverflow(page)
  })
}

test('empty observation windows stay compact and can reveal the catalog', async ({ page }) => {
  await monitorFixtures(page)
  await page.route('**/api/v1/channel-monitor-v2/models*', route => route.fulfill(fulfill({ items: rows.map(row => ({ ...row, metrics: baseMetric })), coverage })))
  await page.route('**/api/v1/channel-monitor-v2/snapshot*', route => route.fulfill(fulfill({ config: { refresh_interval_seconds: 60 }, coverage, metrics: baseMetric, health, trend: [] })))
  await page.route('**/api/v1/channel-monitor-v2/matrix*', route => route.fulfill(fulfill({ items: [], coverage, group_by: 'platform_group' })))
  await page.goto('/home', { waitUntil: 'networkidle' })
  await expect(page.locator('.ag-home-live')).toContainText('当前暂无近期观测')
  await expect(page.locator('.ag-live-groups article')).toHaveCount(0)
  await page.getByRole('checkbox', { name: /显示待观测模型/ }).check()
  await expect(page.locator('.ag-live-groups')).toContainText('unobserved-model')
  await page.goto('/monitor', { waitUntil: 'networkidle' })
  await expect(page.getByText('No recent observations. Include models awaiting observation to see all rows.')).toBeVisible()
  await expect(page.locator('.ag-monitor-models tbody tr')).toHaveCount(0)
  await page.getByRole('checkbox', { name: /Show models awaiting observation/ }).check()
  await expect(page.locator('.ag-monitor-models tbody tr')).toHaveCount(3)
  await expect(page.locator('.ag-monitor-models')).not.toContainText('100.0%')
})

test('home retains the permitted catalog during a failed refresh', async ({ page }) => {
  await monitorFixtures(page)
  await page.goto('/home', { waitUntil: 'networkidle' })
  await expect(page.locator('.ag-live-groups')).toContainText('observed-low-sample')
  await page.route('**/api/v1/channels/available*', route => route.fulfill({ status: 503, json: { message: 'Fixture unavailable' } }))
  await page.locator('.ag-home-live button').click()
  await expect(page.getByRole('alert')).toContainText('可用分组刷新失败')
  await expect(page.locator('.ag-live-groups')).toContainText('observed-low-sample')
})

test('home distinguishes empty permissions from unobserved models', async ({ page }) => {
  await monitorFixtures(page)
  await page.route('**/api/v1/channels/available*', route => route.fulfill(fulfill([])))
  await page.goto('/home', { waitUntil: 'networkidle' })
  await expect(page.getByText('暂无可用模型，请联系管理员确认分组权限。')).toBeVisible()
  await page.getByRole('checkbox', { name: /显示待观测模型/ }).check()
  await expect(page.locator('.ag-live-groups article')).toHaveCount(0)
})

test('token trend renders within the viewport with a single scale', async ({ page }, info) => {
  await prepare(page, 'light', true, 'admin')
  const stats = Object.fromEntries('total_users today_new_users active_users hourly_active_users total_api_keys active_api_keys total_accounts normal_accounts error_accounts ratelimit_accounts overload_accounts total_requests total_input_tokens total_output_tokens total_cache_creation_tokens total_cache_read_tokens total_tokens total_cost total_actual_cost today_requests today_input_tokens today_output_tokens today_cache_creation_tokens today_cache_read_tokens today_tokens today_cost today_actual_cost average_duration_ms uptime rpm tpm'.split(' ').map(key => [key, 0]))
  await page.route('**/api/v1/admin/dashboard/snapshot-v2*', route => route.fulfill(fulfill({ stats, trend: [
    { date: '2026-10-18', requests: 2, input_tokens: 200, output_tokens: 50, cache_creation_tokens: 300, cache_read_tokens: 500000, cost: .1, actual_cost: .05 },
    { date: '2026-10-19', requests: 3, input_tokens: 500, output_tokens: 80, cache_creation_tokens: 100, cache_read_tokens: 900000, cost: .2, actual_cost: .1 },
  ], models: [] })))
  await page.goto('/admin/dashboard', { waitUntil: 'networkidle' })
  const chart = page.getByTestId('dashboard-token-trend')
  await expect(chart.locator('canvas')).toBeVisible()
  await expect(chart).toContainText('All token series share one scale')
  await chart.scrollIntoViewIfNeeded()
  const bounds = (await chart.locator('canvas').boundingBox())!
  expect(bounds.x).toBeGreaterThanOrEqual(0)
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(page.viewportSize()!.width)
  await chart.screenshot({ path: info.outputPath('token-single-axis.png') })
  await noOverflow(page)
})
