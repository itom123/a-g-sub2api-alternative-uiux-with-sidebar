import { test, expect } from '@playwright/test'
import { prepare, settings } from './fixtures'

test('setup guide shows exact IDs and model-specific settings without import actions', async ({ page }, info) => {
  await prepare(page, 'light')
  await page.route('**/v1/models*', route => {
    expect(route.request().headers().authorization).toBe('Bearer example-key-for-theme-verification')
    const catalog = new URL(route.request().url()).searchParams.has('client_version')
    return route.fulfill({ json: catalog ? { models: [{ slug: 'gpt-6-astra', input_modalities: ['text', 'image'], supports_parallel_tool_calls: true, supported_reasoning_levels: [{ effort: 'low' }, { effort: 'high' }, { effort: 'max' }] }] } : { data: [{ id: 'gpt-6-astra' }, { id: 'unreported-model' }] } })
  })
  await page.goto('/keys', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: 'WorkBuddy Setup', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'WorkBuddy Setup' })
  await expect(dialog.getByRole('combobox', { name: 'Model ID' })).toHaveValue('gpt-6-astra')
  await expect(dialog.locator('dl')).toContainText('Low, High, Max')
  await expect(dialog.locator('dl > div').filter({ has: page.locator('dt', { hasText: /^Image Input$/ }) }).locator('dd')).toHaveText('On')
  await expect(dialog).toContainText('Auto (use request-layer default)')
  await expect(dialog).toContainText('Use provider default')
  await expect(dialog).toContainText('/v1/chat/completions')
  await expect(dialog.getByRole('button', { name: /download|import/i })).toHaveCount(0)
  await expect(dialog).toHaveCSS('opacity', '1')
  await expect(dialog.locator('.modal-content')).toHaveCSS('opacity', '1')
  await page.screenshot({ path: info.outputPath('workbuddy-setup.png'), fullPage: true })
  await dialog.getByRole('combobox', { name: 'Model ID' }).selectOption('unreported-model')
  await expect(dialog.locator('dl > div').filter({ has: page.locator('dt', { hasText: /^Image Input$/ }) }).locator('dd')).toHaveText('Not reported')
  await expect(dialog.locator('dl')).not.toContainText('Low, High, Max')
})

test('setup guide reports discovery failure and allows exact manual ID', async ({ page }) => {
  await prepare(page, 'light')
  await page.route('**/v1/models*', route => route.fulfill({ status: 503, json: {} }))
  await page.goto('/keys', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: 'WorkBuddy Setup', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'WorkBuddy Setup' })
  await expect(dialog.getByRole('alert')).toContainText('Model discovery is unavailable')
  await dialog.getByRole('textbox', { name: 'Model ID' }).fill('exact-custom-id')
  await expect(dialog).toContainText('exact-custom-id')
})

for (const group of [
  { id: 8, name: 'openai-低档', platform: 'openai', max_reasoning_effort: 'high', expected: 'Low, Medium, High' },
  { id: 9, name: 'openai-中档', platform: 'openai', max_reasoning_effort: 'xhigh', expected: 'Low, Medium, High, X-High' },
  { id: 10, name: 'openai-高档', platform: 'openai', max_reasoning_effort: '', expected: 'Low, Medium, High, X-High, Max' },
  { id: 11, name: 'Anthropic', platform: 'anthropic', max_reasoning_effort: 'high', expected: 'Low, Medium, High' },
  { id: 12, name: 'Gemini', platform: 'gemini', max_reasoning_effort: '', expected: 'Low, Medium, High, X-High, Max' },
  { id: 13, name: 'Composite', platform: 'composite', max_reasoning_effort: 'high', expected: 'Low, Medium, High' }
]) {
  test(`setup adapts discovery and reasoning to ${group.name}`, async ({ page }) => {
    await prepare(page, 'light')
    await page.route('**/api/v1/settings/public*', route => route.fulfill({ json: { code: 0, data: { ...settings, api_base_url: 'https://upstream.example.test' } } }))
    await page.route('https://upstream.example.test/**', route => route.fulfill({ json: { status: 'ok' } }))
    await page.route('**/api/v1/keys*', route => route.fulfill({ json: { code: 0, data: { items: [{ id: 1, name: 'Group test key', key: 'fixture-group-key', status: 'active', group_id: group.id, group }], total: 1, pages: 1, page: 1, page_size: 20 } } }))
    await page.route('**/v1/models*', route => {
      expect(new URL(route.request().url()).origin).toBe(new URL(page.url()).origin)
      const catalog = new URL(route.request().url()).searchParams.has('client_version')
      return route.fulfill({ json: catalog ? { models: [{ slug: 'exact-model-id', default_reasoning_level: 'max', supported_reasoning_levels: ['low', 'medium', 'high', 'xhigh', 'max'].map(effort => ({ effort })) }] } : { models: [{ slug: 'exact-model-id' }] } })
    })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await page.getByRole('button', { name: 'WorkBuddy Setup', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'WorkBuddy Setup' })
    await expect(dialog.getByRole('combobox', { name: 'Model ID' })).toHaveValue('exact-model-id')
    await expect(dialog).toContainText('Do not omit or change any “-” dash')
    await expect(dialog).toContainText('https://upstream.example.test/v1/chat/completions')
    await expect(dialog.locator('dl > div').filter({ has: page.locator('dt', { hasText: /^Supported reasoning efforts$/ }) }).locator('dd')).toHaveText(group.expected)
    if (group.max_reasoning_effort) await expect(dialog).toContainText('Requests above this ceiling are downgraded')
  })
}

test('uses only the selected group catalog when gateway discovery fails', async ({ page }) => {
  await prepare(page, 'light')
  await page.route('**/v1/models*', route => route.fulfill({ status: 503, json: {} }))
  await page.route('**/api/v1/model-plaza*', route => route.fulfill({ json: { code: 0, data: { groups: [
    { id: 1, models: [{ name: 'group-specific-model' }] },
    { id: 2, models: [{ name: 'wrong-group-model' }] }
  ] } } }))
  await page.goto('/keys', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: 'WorkBuddy Setup', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'WorkBuddy Setup' })
  await expect(dialog.getByRole('combobox', { name: 'Model ID' })).toHaveValue('group-specific-model')
  await expect(dialog).toContainText('Showing the selected group’s model catalog')
  await expect(dialog).not.toContainText('wrong-group-model')
})
