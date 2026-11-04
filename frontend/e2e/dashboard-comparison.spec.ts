import { test, expect } from '@playwright/test'
import { prepare, keyActivityFixture } from './fixtures'
import { assertPairedGeometry } from './paired-trend-geometry'
import type { TrendDataPoint } from '../src/types'

const shots = `${process.env.ART || 'test-results/dashboard-v2'}/shots`
const buckets = { input_tokens: 487191835, cache_creation_tokens: 129913273, cache_read_tokens: 6254396153, output_tokens: 22446094 }
const stats = { total_requests: 38081, total_tokens: 6893947355, total_input_tokens: buckets.input_tokens, total_cache_creation_tokens: buckets.cache_creation_tokens, total_cache_read_tokens: buckets.cache_read_tokens, total_output_tokens: buckets.output_tokens, total_cache_tokens: buckets.cache_creation_tokens + buckets.cache_read_tokens, total_cost: 500, total_actual_cost: 2122.42 / 7, average_duration_ms: 13206.111 }
function points(length: number, hourly = false): TrendDataPoint[] {
  return Array.from({ length }, (_, index) => ({ date: hourly ? `2026-11-01 ${String(index).padStart(2, '0')}:00` : `2026-10-${String(index + 1).padStart(2, '0')}`, requests: 1, input_tokens: 1000000 + index * 1700, output_tokens: 7000, cache_creation_tokens: 80000, cache_read_tokens: 200000000 + index * 1000000, total_tokens: 201087000 + index * 1001700, cost: 10, actual_cost: index % 3 ? 70 + index : 0 }))
}

for (const locale of ['en', 'zh'] as const) for (const theme of ['light', 'dark'] as const) {
  test(`${locale} ${theme} large metrics and aligned native time axes`, async ({ page }, info) => {
    const runtimeErrors: string[] = []
    page.on('pageerror', error => runtimeErrors.push(error.message))
    await prepare(page, theme, true, 'user', locale)
    let trend = points(7)
    await page.route('**/api/v1/usage/stats**', route => route.fulfill({ json: { code: 0, data: stats } }))
    await page.route('**/api/v1/usage/dashboard/stats**', route => route.fulfill({ json: { code: 0, data: { ...stats, total_api_keys: 7, active_api_keys: 7, today_actual_cost: 719.97 / 7, rpm: 2, tpm: 216293, by_platform: [] } } }))
    await page.route('**/api/v1/usage/dashboard/snapshot-v2**', route => route.fulfill({ json: { code: 0, data: { trend, models: [], groups: [], key_activity_status: 'available', key_activity: keyActivityFixture() } } }))
    await page.goto('/dashboard', { waitUntil: 'networkidle' })
    await expect(page.getByTestId('account-stat-grid')).toContainText('6.89B')
    await expect(page.getByTestId('account-stat-grid')).toContainText('13.21s')
    await expect(page.getByTestId('account-stat-grid')).toContainText('2,122.42')
    await expect(page.getByTestId('balance-card')).toContainText('719.97')
    const exact = page.getByTestId('exact-values')
    await exact.locator('summary').click()
    await expect(exact).toContainText('6,893,947,355')
    await expect(exact).toContainText('13,206.111')
    await expect(exact).toContainText('487,191,835')
    await exact.locator('summary').click()
    const geometry = await assertPairedGeometry(page)
    await info.attach('native-chart-geometry', { body: JSON.stringify(geometry, null, 2), contentType: 'application/json' })
    const comparison = page.getByTestId('account-trends')
    await comparison.focus(); await page.keyboard.press('Home')
    await expect(page.getByTestId('account-trend-readout')).toContainText('2026-10-01')
    await page.keyboard.press('ArrowRight')
    await expect(page.getByTestId('account-trend-readout')).toContainText('2026-10-02')
    await page.keyboard.press('Escape')
    const costCanvas = comparison.locator('canvas').first()
    await costCanvas.scrollIntoViewIfNeeded()
    const point = await costCanvas.evaluate(canvas => {
      const chart = (canvas as HTMLCanvasElement & { accountTrendChart?: any }).accountTrendChart
      return { x: chart.scales.x.getPixelForValue(2), y: (chart.chartArea.top + chart.chartArea.bottom) / 2 }
    })
    await costCanvas.hover({ position: point })
    await expect(page.getByTestId('account-trend-readout')).toContainText('2026-10-03')
    await expect(page.getByTestId('account-trend-readout')).toContainText('¥504.00')
    await comparison.getByRole('checkbox').first().click()
    await assertPairedGeometry(page)
    await comparison.getByRole('checkbox').first().click()
    await page.evaluate(() => scrollTo(0, 0))
    await page.screenshot({ path: `${shots}/second-${info.project.name}-${locale}-${theme}-overview.png`, scale: 'css' })
    await page.getByTestId('account-chart-columns').screenshot({ path: `${shots}/second-${info.project.name}-${locale}-${theme}-charts.png`, scale: 'css' })
    const input = comparison.locator('canvas').first()
    await input.scrollIntoViewIfNeeded()
    const cursor = await input.evaluate(canvas => {
      const chart = (canvas as HTMLCanvasElement & { accountTrendChart?: any }).accountTrendChart
      return { x: chart.scales.x.getPixelForValue(2), y: (chart.chartArea.top + chart.chartArea.bottom) / 2 }
    })
    await input.hover({ position: cursor })
    await expect(page.getByTestId('account-trend-readout')).toContainText('2026-10-03')
    await comparison.screenshot({ path: `${shots}/second-${info.project.name}-${locale}-${theme}-crossread.png`, scale: 'css' })
    for (const next of [points(30), points(24, true), points(1), points(7).map(point => ({ ...point, actual_cost: 0, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 0 }))]) {
      trend = next
      await page.getByRole('button', { name: locale === 'zh' ? '刷新' : 'Refresh', exact: true }).last().click()
      await expect.poll(async () => (await assertPairedGeometry(page)).cost.buckets.length).toBe(next.length)
    }
    if (info.project.name === 'desktop') {
      const collapse = page.getByRole('button', { name: locale === 'zh' ? '收起' : 'Collapse', exact: true }).last()
      if (await collapse.isVisible()) { await collapse.click(); await assertPairedGeometry(page) }
    }
    if (info.project.name === 'desktop') for (const width of [1920, 1024, 720, 390]) {
      await page.setViewportSize({ width, height: 1000 })
      await assertPairedGeometry(page)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    }
    trend = []
    await page.getByRole('button', { name: locale === 'zh' ? '刷新' : 'Refresh', exact: true }).last().click()
    await expect(comparison.locator('canvas')).toHaveCount(0)
    trend = points(7)
    await page.goto('/usage', { waitUntil: 'networkidle' })
    await expect(page.locator('canvas').first()).toBeVisible()
    expect(runtimeErrors).toEqual([])
  })
}
