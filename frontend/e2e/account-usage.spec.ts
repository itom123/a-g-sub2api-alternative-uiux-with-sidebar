import { test, expect, type Page, type Request } from '@playwright/test'
import { prepare, settings, user } from './fixtures'

const ARTIFACT_ROOT = process.env.ART || 'test-results/dashboard-v2'
const SHOTS = `${ARTIFACT_ROOT}/shots`

type Harness = {
  requests: Request[]
  failRefresh: () => void
  useLowBalance: () => void
  usePaymentFallback: () => void
}

async function harness(page: Page, theme: 'light' | 'dark'): Promise<Harness> {
  const requests: Request[] = []
  let fail = false
  let lowBalance = false
  let paymentFallback = false
  page.on('request', request => requests.push(request))
  await prepare(page, theme)

  // These handlers intentionally only alter the shared fixture's state. All
  // normal dashboard payloads continue through prepare()'s populated fixture.
  await page.route('**/api/v1/user/profile**', async route => {
    if (fail) return route.fulfill({ status: 503, json: { code: 1, message: 'fixture unavailable' } })
    if (lowBalance) return route.fulfill({ json: { code: 0, data: { ...user, balance: 20, frozen_balance: 10 } } })
    return route.fallback()
  })
  await page.route('**/api/v1/auth/me**', async route => {
    if (fail) return route.fulfill({ status: 503, json: { code: 1, message: 'fixture unavailable' } })
    if (lowBalance) return route.fulfill({ json: { code: 0, data: { ...user, balance: 20, frozen_balance: 10 } } })
    return route.fallback()
  })
  await page.route('**/api/v1/settings/public**', async route => {
    if (paymentFallback) return route.fulfill({ json: { code: 0, data: { ...settings, payment_enabled: false } } })
    return route.fallback()
  })
  await page.route('**/api/v1/currency', route => route.fulfill({ json: { cnyPerUsd: 7, source: 'fixture', rateDate: '2026-10-31', updatedAt: '2026-10-31T00:00:00Z', nextRefreshAt: '2026-12-31T00:00:00Z' } }))
  await page.route('**/api/v1/usage**', async route => {
    if (fail) return route.fulfill({ status: 503, json: { code: 1, message: 'fixture unavailable' } })
    return route.fallback()
  })

  return {
    requests,
    failRefresh: () => { fail = true },
    useLowBalance: () => { lowBalance = true },
    usePaymentFallback: () => { paymentFallback = true },
  }
}

function assertBoundary(page: Page, requests: Request[]) {
  const apiRequests = requests.filter(request => new URL(request.url()).pathname.includes('/api/v1/'))
  expect(apiRequests.filter(request => new URL(request.url()).pathname.includes('/api/v1/admin/'))).toEqual([])
  for (const request of apiRequests) {
    const url = new URL(request.url())
    expect(url.searchParams.has('user_id')).toBe(false)
    const body = request.postData()
    if (!body) continue
    expect(body).not.toMatch(/(?:^|["'&])user_id(?:["'=&#]|$)/i)
    try {
      const parsed = JSON.parse(body) as unknown
      const containsUserId = (value: unknown): boolean => {
        if (!value || typeof value !== 'object') return false
        if (Array.isArray(value)) return value.some(containsUserId)
        return Object.entries(value as Record<string, unknown>).some(([key, child]) => key === 'user_id' || containsUserId(child))
      }
      expect(containsUserId(parsed)).toBe(false)
    } catch {
      // Non-JSON bodies are covered by the key-name assertion above.
    }
  }
}

async function assertRenderedBoundary(page: Page) {
  const body = (await page.locator('body').innerText()).toLowerCase()
  expect(body).not.toMatch(/(?:^|[^a-z])(?:total_|today_)?account_cost(?:[^a-z]|$)/)
  expect(body).not.toContain('upstream cost')
  expect(body).not.toContain('upstream billing')
}

async function waitForChartsSettled(page: Page) {
  // Chart.js animation is disabled by the dashboard, but wait for every canvas
  // to be painted before taking a full-page capture (including dark mode).
  await page.waitForFunction(() => {
    const canvases = Array.from(document.querySelectorAll('canvas')) as HTMLCanvasElement[]
    return canvases.length > 0 && canvases.every((canvas) => {
      const chart = (canvas as HTMLCanvasElement & { __chart?: { isAnimating?: () => boolean } }).__chart
      if (chart?.isAnimating?.()) return false
      const context = canvas.getContext('2d')
      if (!context || canvas.width === 0 || canvas.height === 0) return false
      const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data
      for (let index = 0; index < pixels.length; index += 4) {
        if (pixels[index] !== 0 || pixels[index + 1] !== 0 || pixels[index + 2] !== 0 || pixels[index + 3] !== 0) return true
      }
      return false
    })
  })
  await page.waitForTimeout(250)
}

async function captureFullPage(page: Page, path: string) {
  await page.evaluate(() => window.scrollTo({ top: 0, left: 0, behavior: 'instant' as ScrollBehavior }))
  await waitForChartsSettled(page)
  await page.screenshot({ path, fullPage: true })
}

async function assertActualCostVisible(page: Page, projectName: string) {
  const drilldown = page.getByTestId('account-key-drilldown')
  const daily = page.getByTestId('key-daily-usage')
  const drilldownBox = await drilldown.boundingBox()
  expect(drilldownBox).not.toBeNull()
  if (projectName === 'desktop') {
    const actualHeader = daily.locator('table thead th').last()
    const actualCell = daily.locator('table tbody tr').first().locator('td').last()
    await expect(actualHeader).toBeVisible()
    await expect(actualCell).toBeVisible()
    for (const target of [actualHeader, actualCell]) {
      const box = await target.boundingBox()
      expect(box).not.toBeNull()
      expect(box!.x).toBeGreaterThanOrEqual(drilldownBox!.x - 1)
      expect(box!.y).toBeGreaterThanOrEqual(drilldownBox!.y - 1)
      expect(box!.x + box!.width).toBeLessThanOrEqual(drilldownBox!.x + drilldownBox!.width + 1)
      expect(box!.y + box!.height).toBeLessThanOrEqual(drilldownBox!.y + drilldownBox!.height + 1)
    }
    return
  }

  const mobileDaily = daily.locator('.md\\:hidden').first()
  const actualLabel = mobileDaily.locator('dt').filter({ hasText: /^Actual$/ }).first()
  const actualValue = actualLabel.locator('xpath=following-sibling::dd[1]')
  await expect(actualLabel).toBeVisible()
  await expect(actualValue).toBeVisible()
  for (const target of [actualLabel, actualValue]) {
    const box = await target.boundingBox()
    expect(box).not.toBeNull()
    expect(box!.x).toBeGreaterThanOrEqual(drilldownBox!.x - 1)
    expect(box!.y).toBeGreaterThanOrEqual(drilldownBox!.y - 1)
    expect(box!.x + box!.width).toBeLessThanOrEqual(drilldownBox!.x + drilldownBox!.width + 1)
    expect(box!.y + box!.height).toBeLessThanOrEqual(drilldownBox!.y + drilldownBox!.height + 1)
  }
}

async function assertGroupDonutPainted(page: Page) {
  const painted = await page.getByTestId('account-insights-charts').locator('.account-group-distribution canvas').evaluate((canvas) => {
    const context = (canvas as HTMLCanvasElement).getContext('2d')
    if (!context) return false
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data
    let painted = 0
    for (let index = 0; index < pixels.length; index += 4) {
      if (pixels[index + 3] > 0 && (pixels[index] > 10 || pixels[index + 1] > 10 || pixels[index + 2] > 10)) painted += 1
    }
    return painted > Math.max(40, pixels.length / 4_000)
  })
  expect(painted).toBe(true)
}

async function assertRecentErrorsLayout(page: Page) {
  const card = page.getByTestId('account-errors')
  const cardBox = await card.boundingBox()
  expect(cardBox).not.toBeNull()
  const headers = card.locator('table thead th:visible')
  const expectedLabels = ['Time', 'Model', 'Endpoint', 'Status', 'Category', 'Key', 'Message']
  await expect(headers).toHaveCount(expectedLabels.length)
  const headerBoxes: Record<string, { x: number; y: number; width: number; height: number }> = {}
  for (let index = 0; index < expectedLabels.length; index += 1) {
    const header = headers.nth(index)
    await expect(header).toHaveText(expectedLabels[index])
    const box = await header.boundingBox()
    expect(box).not.toBeNull()
    expect(box!.x).toBeGreaterThanOrEqual(cardBox!.x - 1)
    expect(box!.y).toBeGreaterThanOrEqual(cardBox!.y - 1)
    expect(box!.x + box!.width).toBeLessThanOrEqual(cardBox!.x + cardBox!.width + 1)
    expect(box!.y + box!.height).toBeLessThanOrEqual(cardBox!.y + cardBox!.height + 1)
    headerBoxes[expectedLabels[index]] = box!
  }
  const messageCell = card.locator('table tbody tr:visible').first().locator('td').nth(expectedLabels.indexOf('Message'))
  await expect(messageCell).toBeVisible()
  const messageBox = await messageCell.boundingBox()
  expect(messageBox).not.toBeNull()
  expect(messageBox!.x).toBeGreaterThanOrEqual(cardBox!.x - 1)
  expect(messageBox!.y).toBeGreaterThanOrEqual(cardBox!.y - 1)
  expect(messageBox!.x + messageBox!.width).toBeLessThanOrEqual(cardBox!.x + cardBox!.width + 1)
  expect(messageBox!.y + messageBox!.height).toBeLessThanOrEqual(cardBox!.y + cardBox!.height + 1)
  const status = headerBoxes.Status
  const category = headerBoxes.Category
  expect(status.x + status.width <= category.x + 1 || category.x + category.width <= status.x + 1).toBe(true)
}

async function openDashboard(page: Page, theme: 'light' | 'dark') {
  const fixture = await harness(page, theme)
  await page.goto('/dashboard', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('account-usage-page')).toBeVisible()
  await expect(page.getByTestId('account-insights-stats')).toBeVisible()
  // Dashboard charts intentionally disable animation in the component options, but
  // this also makes screenshot capture deterministic if a chart library defaults
  // to an animated transition in a future upgrade.
  await page.evaluate(() => {
    const chart = (window as Window & { Chart?: { defaults?: { animation?: unknown } } }).Chart
    if (chart?.defaults) chart.defaults.animation = false
  })
  await expect(page.locator('[data-testid="account-spending-trend"] canvas')).toBeVisible()
  await waitForChartsSettled(page)
  return fixture
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme} populated dashboard matrix and boundary`, async ({ page }, info) => {
    const fixture = await openDashboard(page, theme)
    await expect(page.getByTestId('account-balance')).toHaveText('¥700.00')
    await expect(page.getByTestId('account-stat-grid').getByText('10,500')).toBeVisible()
    await expect(page.getByTestId('account-stat-grid')).toContainText('380 ms')
    await expect(page.getByTestId('account-spending-trend').locator('canvas')).toBeVisible()
    await expect(page.getByTestId('account-insights-charts')).toContainText('Model Distribution')
    await expect(page.getByTestId('account-insights-charts')).toContainText('Group Usage Distribution')
    await expect(page.getByTestId('quota-bars')).toContainText('85%')
    await expect(page.getByTestId('account-key-ranking')).toContainText('Example key')
    await expect(page.getByTestId('account-errors')).toContainText('Rate limit example')
    if (info.project.name === 'desktop') await assertRecentErrorsLayout(page)
    await expect(page.locator('#account-range-preset')).toHaveValue('7d')
    const presetValues = ['today', '7d', 'thisWeek', 'lastWeek', 'thisMonth', '30d']
    for (const value of presetValues) {
      await page.locator('#account-range-preset').selectOption(value)
      await expect(page.locator('#account-range-preset')).toHaveValue(value)
    }
    await expect(page.locator('#account-range-preset')).toHaveValue('30d')
    await expect(page.locator('html')).toHaveClass(/dark/).catch(() => { if (theme === 'dark') throw new Error('dark theme was not applied') })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
    await assertRenderedBoundary(page)
    assertBoundary(page, fixture.requests)
    const shotName = info.project.name === 'desktop' ? `r9-desktop-${theme}.png` : `r9-phone-${theme}.png`
    await captureFullPage(page, `${SHOTS}/${shotName}`)
    if (theme === 'dark') await assertGroupDonutPainted(page)
  })
}

test('refresh selector persists all five values and pauses while hidden', async ({ page }) => {
  const fixture = await openDashboard(page, 'light')
  const refresh = page.locator('#account-refresh-interval')
  for (const value of ['0', '30', '60', '300', '900']) {
    await refresh.selectOption(value)
    await expect(refresh).toHaveValue(value)
  }
  await refresh.selectOption('300')
  await expect(page.locator('[data-testid="refresh-control"]')).toContainText('Next refresh')
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(page.locator('[data-testid="refresh-control"]')).toContainText('Paused while hidden')
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(page.locator('[data-testid="refresh-control"]')).toContainText('Next refresh')
  await page.reload({ waitUntil: 'networkidle' })
  await expect(refresh).toHaveValue('300')
  await assertRenderedBoundary(page)
  assertBoundary(page, fixture.requests)
})

test('key ranking sort and drill-down expose daily and model data', async ({ page }, info) => {
  const fixture = await openDashboard(page, 'dark')
  const ranking = page.getByTestId('account-key-ranking')
  if (info.project.name === 'phone') await ranking.getByRole('button', { name: 'Open details' }).first().click()
  else await ranking.getByRole('button', { name: 'Calls' }).click()
  await expect(page.getByTestId('account-key-ranking')).toContainText('Example key')
  if (info.project.name !== 'phone') await ranking.getByRole('button', { name: 'Range cost' }).click()
  await ranking.getByRole('button', { name: 'Open details' }).first().click()
  await expect(page.getByTestId('account-key-drilldown')).toBeVisible()
  await expect(page.getByTestId('key-daily-usage')).toContainText('2026-11-01')
  await expect(page.getByTestId('key-model-split')).toContainText('fixture-model')
  await assertActualCostVisible(page, info.project.name)
  if (info.project.name === 'phone') {
    expect(await page.evaluate(() => document.scrollingElement?.scrollWidth <= window.innerWidth)).toBe(true)
  }
  await assertRenderedBoundary(page)
  assertBoundary(page, fixture.requests)
  await captureFullPage(page, `${SHOTS}/${info.project.name === 'desktop' ? 'r9-key-drilldown.png' : 'r9-phone-key-drilldown.png'}`)
})

test('active windows switch, consistency is populated, and capped scans are announced', async ({ page }) => {
  const fixture = await openDashboard(page, 'light')
  const active = page.getByTestId('account-active-now')
  for (const label of ['60 minutes', '12 hours', '24 hours']) {
    await active.getByRole('tab', { name: label }).click()
    await expect(active.getByRole('tab', { name: label })).toHaveAttribute('aria-selected', 'true')
  }
  await expect(page.getByTestId('account-consistency')).toContainText('Active days')
  await expect(page.getByTestId('account-consistency')).not.toContainText('No data')
  await page.route('**/api/v1/usage**', async route => {
    await route.fulfill({ json: { code: 0, data: { items: [], total: 6000, page: 1, page_size: 1000, pages: 6, total_pages: 6 } } })
  })
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByTestId('account-key-ranking')).toContainText('Range scan capped')
  await expect(active).toContainText('5,000-row scan cap')
  await assertRenderedBoundary(page)
  assertBoundary(page, fixture.requests)
})

test('recent errors supports populated, detail request, and empty states', async ({ page }) => {
  const fixture = await openDashboard(page, 'light')
  await expect(page.getByTestId('account-errors')).toContainText('Rate limit example')
  const errorRow = page.getByTestId('account-errors').locator('article:visible, tbody tr:visible').first()
  await expect(errorRow).toBeVisible()
  await errorRow.click()
  await expect.poll(() => fixture.requests.some(request => new URL(request.url()).pathname.endsWith('/usage/errors/10'))).toBe(true)
  await expect(page.getByTestId('account-error-detail')).toBeVisible()
  await page.route('**/api/v1/usage/errors**', async route => {
    if (new URL(route.request().url()).pathname.endsWith('/usage/errors')) {
      await route.fulfill({ json: { code: 0, data: { items: [], total: 0, page: 1, page_size: 10, pages: 1, total_pages: 1 } } })
    } else await route.fallback()
  })
  await expect(page.getByRole('button', { name: 'Refresh', exact: true })).toBeEnabled()
  await expect(page.getByTestId('account-usage-page')).toHaveAttribute('aria-busy', 'false')
  const errorRequestsBeforeRefresh = fixture.requests.filter(request => new URL(request.url()).pathname.endsWith('/usage/errors')).length
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect.poll(() => fixture.requests.filter(request => new URL(request.url()).pathname.endsWith('/usage/errors')).length).toBeGreaterThan(errorRequestsBeforeRefresh)
  await expect(page.getByTestId('account-errors')).toContainText('No recent errors')
  await assertRenderedBoundary(page)
  assertBoundary(page, fixture.requests)
})

test('CSV export has BOM, headers, escaping, and no secret values', async ({ page }) => {
  const fixture = await openDashboard(page, 'light')
  const downloadPromise = page.waitForEvent('download')
  await page.getByTestId('account-key-ranking').getByRole('button', { name: 'Export CSV' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toMatch(/^account-usage-\d{4}-\d{2}-\d{2}-\d{4}-\d{2}-\d{2}\.csv$/)
  const stream = await download.createReadStream()
  expect(stream).not.toBeNull()
  const chunks: Buffer[] = []
  for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
  const csv = Buffer.concat(chunks).toString('utf8')
  expect(csv.startsWith('\ufeffsection,range_start')).toBe(true)
  expect(csv).toContain('"fixture-model,with-comma"')
  expect(csv).not.toContain('secret-must-not-render')
  expect(csv).not.toContain('secret-two')
  await assertRenderedBoundary(page)
  assertBoundary(page, fixture.requests)
})

test('low balance uses purchase and redeem fallback, with an independent screenshot', async ({ page, browser }, info) => {
  const fixture = await harness(page, 'light')
  fixture.useLowBalance()
  await page.goto('/dashboard', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('balance-warning')).toBeVisible()
  await expect(page.getByTestId('account-balance')).toHaveText('¥140.00')
  if (info.project.name === 'desktop') await captureFullPage(page, `${SHOTS}/r9-low-balance.png`)
  await page.getByTestId('balance-warning').getByRole('button', { name: 'Top up' }).click()
  await expect(page).toHaveURL(/\/purchase$/)
  await page.goBack({ waitUntil: 'domcontentloaded' })
  const fallbackPage = await browser.newPage()
  const fallback = await harness(fallbackPage, 'light')
  fallback.useLowBalance()
  await fallbackPage.route('**/api/v1/settings/public**', route => route.fulfill({ json: { code: 0, data: { ...settings, payment_enabled: false } } }))
  // Ordinary users use the non-admin payment config wrapper. Keep this route
  // scoped to the exact endpoint so the boundary assertion proves no admin
  // payment config request is needed for the fallback path.
  await fallbackPage.route('**/api/v1/payment/config**', route => route.fulfill({ json: { code: 0, data: { enabled: false, payment_enabled: false } } }))
  await fallbackPage.evaluate(() => { if (window.__APP_CONFIG__) window.__APP_CONFIG__.payment_enabled = false })
  await fallbackPage.reload({ waitUntil: 'networkidle' })
  await fallbackPage.goto('/dashboard', { waitUntil: 'networkidle' })
  await fallbackPage.getByTestId('balance-warning').getByRole('button', { name: 'Top up' }).click()
  await expect(fallbackPage).toHaveURL(/\/(purchase|redeem)$/)
  await assertRenderedBoundary(fallbackPage)
  assertBoundary(page, [...fixture.requests, ...fallback.requests])
  await fallbackPage.close()
})

test('failed refresh keeps the last good snapshot and account switch cannot show stale account data', async ({ page }) => {
  const fixture = await openDashboard(page, 'light')
  await expect(page.getByTestId('account-balance')).toHaveText('¥700.00')
  fixture.failRefresh()
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('last successful snapshot')
  await expect(page.getByTestId('account-balance')).toHaveText('¥700.00')
  await expect(page.locator('body')).not.toContainText('Other account')
  await assertRenderedBoundary(page)
  assertBoundary(page, fixture.requests)
})

test('dashboard requires a signed-in account', async ({ page }) => {
  await prepare(page, 'light', false)
  await page.goto('/dashboard', { waitUntil: 'networkidle' })
  await expect(page).toHaveURL(/\/login/)
  await expect(page.getByTestId('account-usage-page')).toHaveCount(0)
  assertBoundary(page, [])
})
