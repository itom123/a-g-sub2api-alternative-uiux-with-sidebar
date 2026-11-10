import { test, expect, type Page } from '@playwright/test'
import { prepare } from './fixtures'

// Renders the real WeChat connect dialog with synthetic OpenClaw API data. Screenshots go to
// WECHAT_SHOT_DIR (default: test-results via the test output path). Run with --project=desktop;
// the spec sets its own viewports.
const SETUP_URL = 'https://liteapp.weixin.qq.com/q/7GiQu1?qrcode=0123456789abcdef0123456789abcdef&bot_type=3'
const stamp = '2026-11-10T09:00:00Z'
const instance = { id: 1, tenant_slug: 'u42', name: 'Workspace', status: 'running', worker_host: 'default', created_at: stamp, updated_at: stamp }
const pending = {
  id: 7, instance_id: 1, kind: 'wechat', status: 'pending', label: '', usage_count: 0, created_at: stamp, updated_at: stamp,
  setup_url: SETUP_URL, setup_key: 'main', setup_message: '用手机微信扫描以下二维码，以继续连接：',
}
const connected = { id: 7, instance_id: 1, kind: 'wechat', status: 'connected', label: '', usage_count: 0, created_at: stamp, updated_at: stamp }

async function mockOpenClaw(page: Page) {
  let completeCalls = 0
  const json = (route: import('@playwright/test').Route, data: unknown) => route.fulfill({ json: { code: 0, data } })
  // The dev server proxies /setup/status to a backend that is not running here.
  await page.route('**/setup/status', route => route.fulfill({ json: { code: 0, data: { needs_setup: false } } }))
  await page.route('**/api/v1/openclaw/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/api\/v1/, '')
    const method = route.request().method()
    if (path === '/openclaw/lifecycle') return json(route, { instance, can_create: true, can_delete: true, self_service_enabled: true, hub_name: 'Claw Hub I' })
    if (path === '/openclaw/dashboard') return json(route, { instance, fleet_state: 'running', checked_at: stamp, gateway_enabled: false, enrollment_enabled: false, connection_count: 0, connected_count: 0, usage_count: 0 })
    if (path === '/openclaw/catalog') return json(route, { items: [
      { kind: 'wechat', title: 'WeChat', description: 'Chat with your assistant in WeChat', sort_order: 1, setup_available: true },
      { kind: 'telegram', title: 'Telegram', description: 'Chat with your assistant in Telegram', sort_order: 2, setup_available: true },
    ] })
    if (path === '/openclaw/instances' && method === 'GET') return json(route, { items: [instance] })
    if (path === '/openclaw/instances/1/connections' && method === 'GET') return json(route, { items: [] })
    if (path === '/openclaw/instances/1/connections' && method === 'POST') return json(route, pending)
    if (path === '/openclaw/instances/1/connections/7/complete' && method === 'POST') {
      completeCalls++
      await new Promise(resolve => setTimeout(resolve, 1500))
      return json(route, completeCalls >= 3 ? connected : pending)
    }
    return json(route, { items: [] })
  })
}

const dir = process.env.WECHAT_SHOT_DIR
const shots = [
  { theme: 'light', width: 1440, height: 900 }, { theme: 'dark', width: 1440, height: 900 },
  { theme: 'light', width: 390, height: 844 }, { theme: 'dark', width: 390, height: 844 },
] as const

for (const shot of shots) {
  for (const locale of shot.width === 1440 && shot.theme === 'light' ? ['en', 'zh'] as const : ['en'] as const) {
    test(`WeChat dialog ${locale} ${shot.theme} ${shot.width}`, async ({ page }, info) => {
      test.skip(info.project.name !== 'desktop', 'sets its own viewport')
      const errors: string[] = []
      page.on('console', m => { if (m.type() === 'error') errors.push(m.text()) })
      page.on('response', r => { if (r.status() >= 500) errors.push(`HTTP ${r.status()} ${r.url()}`) })
      page.on('pageerror', e => errors.push(String(e)))
      await page.setViewportSize({ width: shot.width, height: shot.height })
      await prepare(page, shot.theme, true, 'user', locale)
      await mockOpenClaw(page)
      await page.goto('/connections', { waitUntil: 'domcontentloaded' })
      const card = page.locator('.connection-card, article, li').filter({ hasText: 'WeChat' }).filter({ has: page.getByRole('button') }).first()
      await card.getByRole('button').first().click()
      const dialog = page.getByRole('dialog')
      await expect(dialog).toBeVisible()
      const primary = dialog.locator('.dialog-actions .btn-primary')
      await primary.click()
      const qr = dialog.locator('img.wechat-qr-img')
      await expect(qr).toBeVisible()
      await expect(primary).toBeDisabled()
      const suffix = `${shot.theme}-${shot.width}.png`
      const out = (name: string) => dir ? `${dir}/${name}` : info.outputPath(name)
      await page.screenshot({ path: out(locale === 'zh' ? `A-zh-${suffix}` : `A-${suffix}`) })
      if (locale === 'en') {
        const src = await qr.getAttribute('src')
        expect(src).toMatch(/^data:image\//)
        const decoded = await page.evaluate(async (data) => {
          const Detector = (window as unknown as { BarcodeDetector?: new (o: object) => { detect(i: ImageBitmapSource): Promise<{ rawValue: string }[]> } }).BarcodeDetector
          if (!Detector) return null
          const img = new Image(); img.src = data!; await img.decode()
          return (await new Detector({ formats: ['qr_code'] }).detect(img)).map(r => r.rawValue)
        }, src)
        info.annotations.push({ type: 'qr-decode', description: decoded ? JSON.stringify(decoded) : 'skipped: no decoder available' })
        if (decoded) expect(decoded).toEqual([SETUP_URL])
        await expect(dialog.locator('img.wechat-qr-img')).toHaveCount(0, { timeout: 15_000 })
        await expect(dialog).toContainText(/connected/i)
        await page.screenshot({ path: out(`B-${suffix}`) })
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
      expect(errors).toEqual([])
    })
  }
}
