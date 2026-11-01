import type { Page } from '@playwright/test'

export const user = {
  id: 42, username: 'Account fixture', email: 'account@example.invalid', role: 'user',
  balance: 100, frozen_balance: 10, concurrency: 5, status: 'active', allowed_groups: [],
  balance_notify_enabled: false, balance_notify_threshold: 10, balance_notify_extra_emails: [],
  created_at: '2026-01-01T09:00:00Z', updated_at: '2026-01-01T09:00:00Z',
}

export const settings = {
  site_name: 'AG_>', site_logo: '', site_subtitle: 'API portal', api_base_url: '',
  registration_enabled: true, password_reset_enabled: true, email_verify_enabled: false,
  turnstile_enabled: false, tencent_captcha_enabled: false, aliyun_captcha_enabled: false,
  backend_mode_enabled: false, compact_home_enabled: true, payment_enabled: true,
  affiliate_enabled: true, subscription_enabled: true, model_plaza_enabled: true,
  plugin_management_enabled: true, service_quota_enabled: true, available_channels_enabled: true,
  channel_monitor_enabled: true, channel_monitor_mode: 'v2', ops_monitoring_enabled: true,
  ops_realtime_monitoring_enabled: false, custom_menu_items: [{ id: 'theme-example', label: 'Example guide', icon_svg: '', url: 'md:theme-example', visibility: 'user', sort_order: 1 }], custom_endpoints: [],
  table_default_page_size: 20, table_page_size_options: [10, 20, 50],
  registration_email_suffix_whitelist: [], login_agreement_documents: [{ id: 'terms', title: 'Terms of service', content_md: '# Terms\n\nReadable terms in either theme.\n\n```text\nExample text\n```' }],
}

export const profile = {
  user_id: user.id, display_name: 'Account fixture', roles: 'Producer, arts administrator',
  locations: 'London, UK remote', remote_ok: true, salary_min: 35000,
  timezone: 'Europe/London', notes: 'Creative technology and accessible workplaces.', telegram_paired: false,
}

type FixtureUsageRow = {
  id: number
  api_key_id: number
  group_id: number
  model: string
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  total_tokens: number
  actual_cost: number
  total_cost: number
  created_at: string
  duration_ms: number
}

type FixtureDay = {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  actual_cost: number
  total_cost: number
  keyOneRequests: number
}

// The selected 7-day range is one internally consistent ledger: every endpoint
// below is derived from these same daily buckets and therefore reconciles.
const daySpecs: FixtureDay[] = [
  { date: '2026-10-26', requests: 4, input_tokens: 800, output_tokens: 160, cache_creation_tokens: 40, cache_read_tokens: 200, actual_cost: 0.30, total_cost: 0.40, keyOneRequests: 3 },
  { date: '2026-10-27', requests: 5, input_tokens: 900, output_tokens: 180, cache_creation_tokens: 40, cache_read_tokens: 180, actual_cost: 0.35, total_cost: 0.50, keyOneRequests: 3 },
  { date: '2026-10-28', requests: 6, input_tokens: 950, output_tokens: 200, cache_creation_tokens: 40, cache_read_tokens: 210, actual_cost: 0.40, total_cost: 0.55, keyOneRequests: 4 },
  { date: '2026-10-29', requests: 7, input_tokens: 1_000, output_tokens: 220, cache_creation_tokens: 40, cache_read_tokens: 240, actual_cost: 0.45, total_cost: 0.60, keyOneRequests: 4 },
  { date: '2026-10-30', requests: 8, input_tokens: 1_050, output_tokens: 240, cache_creation_tokens: 40, cache_read_tokens: 270, actual_cost: 0.50, total_cost: 0.65, keyOneRequests: 5 },
  { date: '2026-10-31', requests: 5, input_tokens: 1_150, output_tokens: 250, cache_creation_tokens: 50, cache_read_tokens: 350, actual_cost: 0.55, total_cost: 0.75, keyOneRequests: 3 },
  { date: '2026-11-01', requests: 5, input_tokens: 1_150, output_tokens: 250, cache_creation_tokens: 50, cache_read_tokens: 250, actual_cost: 0.45, total_cost: 0.55, keyOneRequests: 2 },
]

function splitInteger(total: number, count: number, index: number): number {
  const base = Math.floor(total / count)
  return index === count - 1 ? total - base * (count - 1) : base
}

function splitMoney(total: number, count: number, index: number): number {
  const micros = Math.round(total * 1_000_000)
  const base = Math.floor(micros / count)
  const value = index === count - 1 ? micros - base * (count - 1) : base
  return value / 1_000_000
}

// Keep the chart buckets deterministic, but place the raw usage rows relative to the
// test clock. Active-now deliberately has rows in each exact window (60m, 12h, 24h)
// instead of serving timestamps that become stale as the suite moves forward.
const fixtureNow = Date.now()
const usageRows: FixtureUsageRow[] = []
let nextRowId = 1
for (const [dayIndex, day] of daySpecs.entries()) {
  for (let requestIndex = 0; requestIndex < day.requests; requestIndex += 1) {
    const apiKeyId = requestIndex < day.keyOneRequests ? 1 : 2
    const model = apiKeyId === 1 ? 'fixture-model,with-comma' : 'second fixture model'
    const groupId = apiKeyId === 1 ? 1 : 2
    const inputTokens = splitInteger(day.input_tokens, day.requests, requestIndex)
    const outputTokens = splitInteger(day.output_tokens, day.requests, requestIndex)
    const cacheCreationTokens = splitInteger(day.cache_creation_tokens, day.requests, requestIndex)
    const cacheReadTokens = splitInteger(day.cache_read_tokens, day.requests, requestIndex)
    const totalTokens = inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
    const offsetMinutes = dayIndex === 0
      ? 8 + requestIndex * 7
      : dayIndex === 1
        ? 120 + requestIndex * 11
        : dayIndex === 2
          ? 14 * 60 + requestIndex * 13
          : dayIndex * 24 * 60 + requestIndex * 17
    usageRows.push({
      id: nextRowId++, api_key_id: apiKeyId, group_id: groupId, model,
      input_tokens: inputTokens, output_tokens: outputTokens,
      cache_read_tokens: cacheReadTokens, cache_creation_tokens: cacheCreationTokens,
      total_tokens: totalTokens, actual_cost: splitMoney(day.actual_cost, day.requests, requestIndex),
      total_cost: splitMoney(day.total_cost, day.requests, requestIndex),
      created_at: new Date(fixtureNow - offsetMinutes * 60_000).toISOString(),
      duration_ms: 380,
    })
  }
}

function summarizeRows(rows: FixtureUsageRow[]) {
  return rows.reduce((summary, row) => ({
    requests: summary.requests + 1,
    input_tokens: summary.input_tokens + row.input_tokens,
    output_tokens: summary.output_tokens + row.output_tokens,
    cache_creation_tokens: summary.cache_creation_tokens + row.cache_creation_tokens,
    cache_read_tokens: summary.cache_read_tokens + row.cache_read_tokens,
    total_tokens: summary.total_tokens + row.total_tokens,
    actual_cost: summary.actual_cost + row.actual_cost,
    cost: summary.cost + row.total_cost,
  }), { requests: 0, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 0, actual_cost: 0, cost: 0 })
}

function modelStatsFor(rows: FixtureUsageRow[]) {
  const byModel = new Map<string, FixtureUsageRow[]>()
  for (const row of rows) byModel.set(row.model, [...(byModel.get(row.model) ?? []), row])
  return [...byModel.entries()].map(([model, modelRows]) => {
    const summary = summarizeRows(modelRows)
    return { model, requests: summary.requests, input_tokens: summary.input_tokens, output_tokens: summary.output_tokens, cache_creation_tokens: summary.cache_creation_tokens, cache_read_tokens: summary.cache_read_tokens, total_tokens: summary.total_tokens, actual_cost: summary.actual_cost, cost: summary.cost }
  })
}

function groupStatsFor(rows: FixtureUsageRow[]) {
  const byGroup = new Map<number, FixtureUsageRow[]>()
  for (const row of rows) byGroup.set(row.group_id, [...(byGroup.get(row.group_id) ?? []), row])
  return [...byGroup.entries()].map(([groupId, groupRows]) => {
    const summary = summarizeRows(groupRows)
    return { group_id: groupId, group_name: groupId === 1 ? 'Standard API group' : 'Research API group', requests: summary.requests, total_tokens: summary.total_tokens, actual_cost: summary.actual_cost, cost: summary.cost }
  })
}

const trend = daySpecs.map((day) => ({
  date: day.date, requests: day.requests, input_tokens: day.input_tokens, output_tokens: day.output_tokens,
  cache_creation_tokens: day.cache_creation_tokens, cache_read_tokens: day.cache_read_tokens,
  total_tokens: day.input_tokens + day.output_tokens + day.cache_creation_tokens + day.cache_read_tokens,
  cost: day.total_cost, actual_cost: day.actual_cost,
}))
const rangeSummary = summarizeRows(usageRows)
const models = modelStatsFor(usageRows)
const groups = groupStatsFor(usageRows)
const rangeStats = {
  total_requests: rangeSummary.requests, total_input_tokens: rangeSummary.input_tokens, total_output_tokens: rangeSummary.output_tokens,
  total_cache_tokens: rangeSummary.cache_creation_tokens + rangeSummary.cache_read_tokens,
  total_cache_creation_tokens: rangeSummary.cache_creation_tokens, total_cache_read_tokens: rangeSummary.cache_read_tokens,
  total_tokens: rangeSummary.total_tokens, total_cost: rangeSummary.cost, total_actual_cost: rangeSummary.actual_cost,
  average_duration_ms: Math.round(usageRows.reduce((sum, row) => sum + row.duration_ms, 0) / usageRows.length),
}

function dailyForKey(apiKeyId: number) {
  const byDate = new Map<string, FixtureUsageRow[]>()
  for (const row of usageRows.filter((item) => item.api_key_id === apiKeyId)) byDate.set(row.created_at.slice(0, 10), [...(byDate.get(row.created_at.slice(0, 10)) ?? []), row])
  return {
    items: [...byDate.entries()].map(([date, rows]) => {
      const summary = summarizeRows(rows)
      return { date, requests: summary.requests, input_tokens: summary.input_tokens, output_tokens: summary.output_tokens, cache_read_tokens: summary.cache_read_tokens, cache_write_tokens: summary.cache_creation_tokens, total_tokens: summary.total_tokens, cost: summary.cost, actual_cost: summary.actual_cost }
    }),
    days: 30, start_date: '2026-10-03', end_date: '2026-11-01',
  }
}

const stats = {
  total_api_keys: 2, active_api_keys: 2, total_requests: rangeSummary.requests, total_input_tokens: rangeSummary.input_tokens, total_output_tokens: rangeSummary.output_tokens,
  total_cache_creation_tokens: rangeSummary.cache_creation_tokens, total_cache_read_tokens: rangeSummary.cache_read_tokens, total_tokens: rangeSummary.total_tokens,
  total_cost: rangeSummary.cost, total_actual_cost: rangeSummary.actual_cost,
  today_requests: daySpecs.at(-1)!.requests, today_input_tokens: daySpecs.at(-1)!.input_tokens, today_output_tokens: daySpecs.at(-1)!.output_tokens,
  today_cache_creation_tokens: daySpecs.at(-1)!.cache_creation_tokens, today_cache_read_tokens: daySpecs.at(-1)!.cache_read_tokens,
  today_tokens: trend.at(-1)!.total_tokens, today_cost: daySpecs.at(-1)!.total_cost, today_actual_cost: daySpecs.at(-1)!.actual_cost,
  average_duration_ms: rangeStats.average_duration_ms, rpm: 3, tpm: 620,
  by_platform: [{ platform: 'openai', total_requests: rangeSummary.requests, total_tokens: rangeSummary.total_tokens, total_actual_cost: rangeSummary.actual_cost, today_requests: daySpecs.at(-1)!.requests, today_tokens: trend.at(-1)!.total_tokens, today_actual_cost: daySpecs.at(-1)!.actual_cost }],
}
const empty = { items: [], total: 0, page: 1, page_size: 20, pages: 1, total_pages: 1 }
const coverage = { requested_start: '2026-01-01T00:00:00Z', coverage_start: '2026-01-01T00:00:00Z', data_through: '2026-01-02T00:00:00Z', computed_at: '2026-01-02T00:00:00Z', aggregation_lag_seconds: 0, coverage_complete: true, bucket_seconds: 60 }
const monitorConfig = { version: 1, enabled: true, refresh_interval_seconds: 60, platforms: [], group_ids: [], ignored_error_categories: [], health_thresholds: { minimum_sample: 10, warning_error_rate: 0.05, critical_error_rate: 0.1, target_ttft_ms: 1000, warning_ttft_ms: 3000, critical_ttft_ms: 10000, warning_cache_rate: 0.5, critical_cache_rate: 0.1, error_weight: 1, ttft_weight: 1, cache_weight: 1 } }
const group = { id: 1, name: 'Standard API group', platform: 'openai', status: 'active', rate_multiplier: 1, is_exclusive: false, description: 'Shared API usage', created_at: user.created_at, updated_at: user.updated_at }
const secondGroup = { id: 2, name: 'Research API group', platform: 'openai', status: 'active', rate_multiplier: 1, is_exclusive: false, description: 'Research usage', created_at: user.created_at, updated_at: user.updated_at }
const keys = [
  { id: 1, user_id: user.id, name: 'Example key', key: 'secret-must-not-render', status: 'active', quota: 100, quota_used: 20, group_id: 1, group, last_used_at: '2026-11-01T11:30:00Z', created_at: user.created_at, updated_at: user.updated_at },
  { id: 2, user_id: user.id, name: 'Second key', key: 'secret-two', status: 'active', quota: 100, quota_used: 10, group_id: 2, group: secondGroup, last_used_at: '2026-11-01T12:00:00Z', created_at: user.created_at, updated_at: user.updated_at },
]
const errors = [{ id: 10, created_at: '2026-11-01T10:00:00Z', model: 'fixture-model,with-comma', inbound_endpoint: '/v1/chat/completions', status_code: 429, category: 'rate_limit', platform: 'openai', message: 'Rate limit example', key_name: 'Example key', error_body: 'upstream body', upstream_status: 429 }]
const quota = { platform_quotas: [{ platform: 'openai', daily_limit_usd: 10, daily_usage_usd: 8.5, weekly_limit_usd: 50, weekly_usage_usd: 20, monthly_limit_usd: null, monthly_usage_usd: 0, daily_window_resets_at: '2026-11-02T00:00:00Z' }] }

function response(path: string): unknown {
  if (path === '/currency') return { cnyPerUsd: 7, source: 'fixture', rateDate: '2026-10-31', updatedAt: '2026-10-31T00:00:00Z', nextRefreshAt: '2026-12-31T00:00:00Z' }
  if (path === '/settings/public' || path === '/admin/settings') return settings
  if (path === '/auth/me' || path === '/user/profile') return user
  if (path === '/keys') return { ...empty, page_size: 1000, total: keys.length, items: keys, pages: 1, total_pages: 1 }
  if (path === '/user/platform-quotas') return quota
  if (path === '/usage/stats') return rangeStats
  if (path === '/usage/dashboard/stats') return stats
  if (path === '/usage/dashboard/trend') return { trend }
  if (path === '/usage/dashboard/snapshot-v2') return { generated_at: '2026-11-01T12:00:00Z', start_date: '2026-10-26', end_date: '2026-11-01', granularity: 'day', trend, models, groups }
  if (path === '/usage/dashboard/models') return { models }
  if (path === '/usage/dashboard/api-keys-usage') return { stats: { '1': { api_key_id: 1, today_actual_cost: 0.18, total_actual_cost: 1.8 }, '2': { api_key_id: 2, today_actual_cost: 0.27, total_actual_cost: 1.2 } } }
  if (path === '/user/api-keys/1/usage/daily') return dailyForKey(1)
  if (path === '/user/api-keys/2/usage/daily') return dailyForKey(2)
  if (path === '/usage') return { ...empty, page_size: 1000, total: usageRows.length, items: usageRows, pages: 1, total_pages: 1 }
  if (path === '/usage/errors') return { ...empty, page_size: 10, total: errors.length, items: errors, pages: 1, total_pages: 1 }
  if (path.startsWith('/usage/errors/')) return errors[0]
  if (path === '/admin/users') return { ...empty, total: 1, items: [{ ...user, notes: '', group_rates: {} }] }
  if (path === '/admin/groups') return { ...empty, total: 2, items: [group, secondGroup] }
  if (path === '/admin/groups/all' || path === '/groups/available') return [group, secondGroup]
  if (path === '/setup/status') return { needs_setup: true }
  if (path === '/openclaw/career/profile') return profile
  if (path === '/openclaw/career/schedules') return { items: [] }
  if (path === '/openclaw/lifecycle') return { can_create: true, self_service_enabled: true, hub_name: 'Claw Hub I' }
  if (/\/openclaw\/(instances|catalog|access-requests)$/.test(path)) return empty
  if (path === '/admin/openclaw/summary') return { total: 0, running: 0, pending: 0, failed: 0 }
  if (path.includes('/dashboard/') || path.startsWith('/usage/')) return { ...stats, stats, trend, models, groups, ranking: [], items: usageRows, ...rangeStats, stats_updated_at: '2026-11-01T09:00:00Z', start_date: '2026-01-01', end_date: '2026-11-01', granularity: 'day' }
  if (path === '/payment/checkout-info') return { methods: {}, global_min: 0, global_max: 0, subscription_allowed: true, plans: [], balance_disabled: false, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0, recharge_bonus_tiers: [], recharge_bonus_mode: 'bonus', recharge_bonus_notice: '', help_text: '', help_image_url: '', stripe_publishable_key: '', alipay_force_qrcode: false, alipay_mobile_precreate_deep_link: false }
  if (path === '/payment/access') return { allowed: true }
  if (path === '/payment/sessions/my') return []
  if (path === '/payment/config') return { enabled: true, payment_methods: [], payment_types: [] }
  if (path === '/subscriptions/summary') return { active_count: 0, subscriptions: [] }
  if (path === '/admin/plugins') return []
  if (path === '/admin/payment/dashboard') return { today_amount: {}, total_amount: {}, avg_amount: {}, today_count: 0, total_count: 0, daily_series: [], payment_methods: [], top_users: {} }
  if (path === '/user/auth-bindings' || path === '/user/account-bindings') return { providers: [], bindings: [] }
  if (path.includes('/passkeys') || path.includes('/user-attributes')) return []
  if (path.includes('/totp/status')) return { enabled: false }
  if (path.includes('/affiliate') || path === '/user/aff') return { ...empty, enabled: true, balance: 0, total_commission: 0, total_referred: 0, commission_rate: 0.1, aff_count: 0, aff_quota: 0, aff_frozen_quota: 0, aff_history_quota: 0, aff_code: 'EXAMPLE', invitees: [], effective_rebate_rate_percent: 10 }
  if (path === '/model-plaza') return { models: [], groups: [], platforms: [], generated_at: '2026-01-01T09:00:00Z' }
  if (path.endsWith('/channel-monitor-v2/config')) return monitorConfig
  if (path.includes('/channel-monitor')) return { config: monitorConfig, coverage, metrics: { request_count: 0, success_requests: 0, error_requests: 0, token_count: 0, rpm: 0, tpm: 0, error_rate: 0, cache_rate: 0, cache_rate_numerator: 0, cache_rate_denominator: 0, ttft: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null }, duration: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null }, }, health: { overall: 'unknown', minimum_sample: 10, error_rate: 'unknown', ttft: 'unknown' }, platforms: [], models: [], groups: [], users: [], trend: [], summary: {}, items: [], updated_at: '2026-01-01T09:00:00Z' }
  if (/\/(announcements|active|all|plans|platforms|providers|custom-menu-items|available|groups|models)$/.test(path) && !['/admin/groups', '/admin/announcements'].includes(path)) return []
  return empty
}

export async function prepare(page: Page, theme: 'light' | 'dark', authenticated = true, role: 'user' | 'admin' = 'user') {
  const currentUser = { ...user, role }
  await page.addInitScript(({ theme, authenticated, user, settings }) => {
    // Init scripts can run while the document is still an about:blank-like shell.
    // Never dereference the DOM or storage until the relevant host is available.
    const applyTheme = () => {
      if (typeof document === 'undefined') return
      const root = document.documentElement
      if (!root) return
      root.classList.toggle('dark', theme === 'dark')
    }
    const applyStorage = () => {
      if (typeof window === 'undefined') return
      try {
        const storage = window.localStorage
        if (!storage) return
        storage.setItem('theme', theme)
        storage.setItem('locale', 'en')
        storage.setItem(`admin_guide_${user.id}_admin_v4_interactive`, 'true')
        storage.setItem(`user_guide_${user.id}_user_v4_interactive`, 'true')
        if (authenticated) {
          storage.setItem('auth_token', 'theme-fixture-token')
          storage.setItem('auth_user', JSON.stringify(user))
        } else {
          storage.removeItem('auth_token')
          storage.removeItem('auth_user')
        }
      } catch {
        // Storage is unavailable in an opaque pre-document context; the later
        // same-origin navigation seeds it in prepare() below.
      }
    }
    applyTheme()
    applyStorage()
    if (typeof document !== 'undefined') {
      document.addEventListener('DOMContentLoaded', () => {
        applyTheme()
        applyStorage()
      }, { once: true })
    }
    if (typeof window !== 'undefined') {
      Object.defineProperty(window, '__APP_CONFIG__', { get: () => settings, set: () => {} })
      // The router restores auth during the first navigation. Keep fixture storage stable.
      if (authenticated) {
        window.addEventListener('storage', () => {
          try {
            const storage = window.localStorage
            if (!storage) return
            if (!storage.getItem('auth_token')) storage.setItem('auth_token', 'theme-fixture-token')
            if (!storage.getItem('auth_user')) storage.setItem('auth_user', JSON.stringify(user))
          } catch {
            // Ignore storage events while the document has no same-origin storage.
          }
        })
      }
    }
  }, { theme, authenticated, user: currentUser, settings })
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, '')
    if (path === '/pages/theme-example') {
      await route.fulfill({ contentType: 'text/markdown', body: '# Example guide\n\nReadable custom content.\n\n```text\nexample code\n```' })
      return
    }
    if (path === '/currency') return route.fulfill({ json: response(path) as object })
    await route.fulfill({ json: { code: 0, data: path === '/auth/me' || path === '/user/profile' ? currentUser : response(path) } })
  })
  await page.route('**/setup', async route => { const document = await route.fetch({ url: new URL('/index.html', route.request().url()).href }); await route.fulfill({ response: document }) })
  // Seed storage from a same-origin document to avoid opaque about:blank storage semantics.
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  await page.evaluate(({ theme, authenticated, user }) => {
    localStorage.setItem('theme', theme)
    const root = document.documentElement
    if (root) root.classList.toggle('dark', theme === 'dark')
    if (authenticated) {
      localStorage.setItem('auth_token', 'theme-fixture-token')
      localStorage.setItem('auth_user', JSON.stringify(user))
    } else {
      localStorage.removeItem('auth_token')
      localStorage.removeItem('auth_user')
    }
  }, { theme, authenticated, user: currentUser })
}
