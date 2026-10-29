import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  workers: 2,
  timeout: 45_000,
  expect: { timeout: 8_000 },
  reporter: [['list'], ['html', { open: 'never' }], ['json', { outputFile: 'test-results/results.json' }]],
  use: {
    baseURL: process.env.PORTAL_BASE_URL || 'http://127.0.0.1:4173',
    headless: true,
    bypassCSP: true,
    launchOptions: process.env.PORTAL_CHROME_CHANNEL ? { channel: process.env.PORTAL_CHROME_CHANNEL } : {},
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'phone', use: { ...devices['iPhone 13'], defaultBrowserType: 'chromium' } },
    { name: 'desktop', use: { viewport: { width: 1440, height: 1000 } } },
  ],
  webServer: process.env.PORTAL_BASE_URL ? undefined : {
    command: 'PORTAL_UI_ACCEPTANCE=1 node node_modules/vite/bin/vite.js --config vite.config.ts --host 127.0.0.1 --port 4173 --strictPort',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
})
