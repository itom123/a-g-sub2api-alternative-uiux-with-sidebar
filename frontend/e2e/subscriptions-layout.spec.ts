import { test, expect } from '@playwright/test'
import { prepare, user, subscriptionFixtures } from './fixtures'

const shots = `${process.env.ART || 'test-results/dashboard-v2'}/shots`
for (const theme of ['light', 'dark'] as const) test(`${theme} admin subscription quota columns align`, async ({ page }, info) => {
  if (info.project.name === 'desktop') await page.setViewportSize({ width: 1920, height: 1080 })
  await prepare(page, theme, true, 'admin', 'zh')
  const rows = Array.from({ length: 5 }, (_, index) => ({ ...subscriptionFixtures[0], id: 500 + index, user_id: 70 + index,
    user: { ...user, id: 70 + index, email: `fixture${index}@example.invalid`, username: `Example ${index}` },
    group: { ...subscriptionFixtures[0].group, name: `订阅套餐 ${index + 1}`, daily_limit_usd: 10 + index * 5, weekly_limit_usd: 50 + index * 5, monthly_limit_usd: 200 + index * 50 },
    daily_usage_usd: index, weekly_usage_usd: index * 10, monthly_usage_usd: index * 20,
  }))
  await page.route('**/api/v1/admin/subscriptions**', route => route.fulfill({ json: { code: 0, data: { items: rows, total: rows.length, pages: 1, page: 1, page_size: 50 } } }))
  await page.goto('/admin/subscriptions', { waitUntil: 'networkidle' })
  await expect(page.locator('.subscription-usage-cell')).toHaveCount(15)
  if (info.project.name === 'desktop') {
    const geometry = await page.locator('tbody tr').evaluateAll(rows => rows.map(row => [...row.querySelectorAll('.subscription-usage-cell')].map(cell => ({ box:cell.getBoundingClientRect().toJSON(),amount:cell.querySelector('.subscription-usage-cell__amount')!.getBoundingClientRect().toJSON(),subline:cell.querySelector('.subscription-usage-cell__window')!.getBoundingClientRect().toJSON() }))))
    expect(geometry).toHaveLength(5)
    geometry.forEach(cells => {
      expect(cells).toHaveLength(3)
      for (const cell of cells) {
        expect(Math.abs(cell.amount.y - cells[0].amount.y)).toBeLessThanOrEqual(1)
        expect(Math.abs(cell.subline.y - cells[0].subline.y)).toBeLessThanOrEqual(1)
        expect(Math.abs(cell.box.width - cells[0].box.width)).toBeLessThanOrEqual(1)
      }
    })
    await info.attach('subscription-column-geometry', {body:JSON.stringify(geometry,null,2), contentType:'application/json'})
    await page.setViewportSize({ width: 1440, height: 1000 })
    const scroll = page.locator('.table-wrapper').first()
    await scroll.evaluate(element => { element.scrollLeft = element.scrollWidth })
    await expect(page.locator('th[data-column="monthly_usage"]')).toBeInViewport()
    await expect(page.locator('th[data-column="expires_at"]')).toBeInViewport()
    await page.setViewportSize({ width: 1920, height: 1080 })
    await scroll.evaluate(element => { element.scrollLeft = 0 })
  } else {
    await expect(page.locator('[data-field="daily_usage"]')).toHaveCount(5)
    await expect(page.locator('[data-field="weekly_usage"]')).toHaveCount(5)
    await expect(page.locator('[data-field="monthly_usage"]')).toHaveCount(5)
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
  await page.screenshot({ path: `${shots}/third-subscriptions-${info.project.name}-${theme}.png`, fullPage: true, scale:'css' })
})
