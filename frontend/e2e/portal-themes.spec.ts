import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { prepare } from './fixtures'
import { audit } from './audit'

// Discover component routes so newly added portal pages automatically join the audit.
const source = readFileSync(new URL('../src/router/index.ts', import.meta.url), 'utf8')
const routes = [...source.matchAll(/path: '([^']+)'([\s\S]*?)(?=\n {2}\{|\n\])/g)]
  .filter(([, path, block]) => block.includes('component:') && !path.includes(':') && !path.startsWith('/auth/'))
  .map(([, path, block]) => ({ path, authenticated: !block.includes('requiresAuth: false') }))
routes.push({ path: '/legal/terms', authenticated: false }, { path: '/custom/theme-example', authenticated: true })

for (const theme of ['light', 'dark'] as const) {
  for (const route of routes) {
    test(`${theme} ${route.path}`, async ({ page }, info) => {
      const errors: string[] = []
      page.on('pageerror', error => errors.push(error.message))
      await prepare(page, theme, route.authenticated, route.path.startsWith('/admin') ? 'admin' : 'user')
      await page.goto(route.path, { waitUntil: 'networkidle' })
      expect(errors, 'Uncaught startup errors').toEqual([])
      await expect(page.locator('#app')).not.toBeEmpty()
      await expect(page).toHaveURL(new RegExp(`${route.path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?:\\?|$)`))
      await expect(page.locator('html')).toHaveClass(theme === 'dark' ? /dark/ : /^(?!.*\bdark\b)/)
      expect(errors, 'Uncaught rendering errors').toEqual([])
      await audit(page, info)
    })
  }

  for (const width of [320, 768, 1024]) {
    test(`${theme} career at ${width}px`, async ({ page }, info) => {
      await page.setViewportSize({ width, height: 900 })
      await prepare(page, theme)
      await page.goto('/career-concierge', { waitUntil: 'networkidle' })
      await expect(page.getByRole('heading', { name: 'Personalised job advice' })).toBeVisible()
      await expect(page.locator('.form-grid input').first()).toHaveValue('Theme verification')
      await expect(page.getByRole('button', { name: 'Add schedule', exact: true })).toBeDisabled()
      const field = page.getByRole('textbox', { name: 'Cron expression' })
      expect((await field.boundingBox())!.width).toBeGreaterThan(100)
      const surfaces = await page.locator('.panel, .form-grid input, .form-grid textarea').evaluateAll(elements => elements.map(el => ({ background: getComputedStyle(el).backgroundColor, color: getComputedStyle(el).color })))
      for (const surface of surfaces) {
        expect(surface.background).toBe(theme === 'dark' ? 'rgb(0, 0, 0)' : 'rgb(255, 255, 255)')
        expect(surface.color).toBe(theme === 'dark' ? 'rgb(255, 255, 255)' : 'rgb(0, 0, 0)')
      }
      await audit(page, info)
    })
  }
}

test('theme toggle persists after reload', async ({ page }) => {
  await prepare(page, 'light', false)
  await page.goto('/login', { waitUntil: 'networkidle' })
  await page.getByRole('button', { name: 'Dark Mode', exact: true }).click()
  await expect(page.locator('html')).toHaveClass(/dark/)
  await page.reload({ waitUntil: 'networkidle' })
  await expect(page.locator('html')).toHaveClass(/dark/)
  await page.getByRole('button', { name: 'Light Mode', exact: true }).click()
  await page.reload({ waitUntil: 'networkidle' })
  await expect(page.locator('html')).not.toHaveClass(/dark/)
})

test('system preference applies when no theme is saved', async ({ page }) => {
  await prepare(page, 'light', false)
  await page.addInitScript(() => localStorage.removeItem('theme'))
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto('/login', { waitUntil: 'networkidle' })
  await expect(page.locator('html')).toHaveClass(/dark/)
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(page.locator('html')).not.toHaveClass(/dark/)
})
