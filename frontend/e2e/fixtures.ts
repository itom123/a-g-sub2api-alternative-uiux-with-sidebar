import type { Page } from '@playwright/test'

export const user = {
  id: 9001, username: 'Theme verification', email: 'theme@example.invalid', role: 'admin',
  balance: 125, concurrency: 5, status: 'active', allowed_groups: [],
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
  user_id: user.id, display_name: 'Theme verification', roles: 'Producer, arts administrator',
  locations: 'London, UK remote', remote_ok: true, salary_min: 35000,
  timezone: 'Europe/London', notes: 'Creative technology and accessible workplaces.', telegram_paired: false,
}

const stats = Object.fromEntries(`total_users today_new_users active_users hourly_active_users total_api_keys active_api_keys total_accounts normal_accounts error_accounts ratelimit_accounts overload_accounts total_requests total_input_tokens total_output_tokens total_cache_creation_tokens total_cache_read_tokens total_tokens total_cost total_actual_cost today_requests today_input_tokens today_output_tokens today_cache_creation_tokens today_cache_read_tokens today_tokens today_cost today_actual_cost average_duration_ms uptime rpm tpm`.split(' ').map(key => [key, 0]))
const empty = { items: [], total: 0, page: 1, page_size: 20, pages: 0, total_pages: 0 }
const coverage = { requested_start: '2026-01-01T00:00:00Z', coverage_start: '2026-01-01T00:00:00Z', data_through: '2026-01-02T00:00:00Z', computed_at: '2026-01-02T00:00:00Z', aggregation_lag_seconds: 0, coverage_complete: true, bucket_seconds: 60 }
const monitorConfig = { version: 1, enabled: true, refresh_interval_seconds: 60, platforms: [], group_ids: [], ignored_error_categories: [], health_thresholds: { minimum_sample: 10, warning_error_rate: 0.05, critical_error_rate: 0.1, target_ttft_ms: 1000, warning_ttft_ms: 3000, critical_ttft_ms: 10000, warning_cache_rate: 0.5, critical_cache_rate: 0.1, error_weight: 1, ttft_weight: 1, cache_weight: 1 } }
const group = { id: 1, name: 'Standard API group', platform: 'openai', status: 'active', rate_multiplier: 1, is_exclusive: false, description: 'Shared API usage', created_at: user.created_at, updated_at: user.updated_at }

function response(path: string): unknown {
  if (path === '/settings/public' || path === '/admin/settings') return settings
  if (path === '/auth/me' || path === '/user/profile') return user
  if (path === '/keys') return { ...empty, total: 1, items: [{ id: 1, user_id: user.id, name: 'Example key for theme verification', key: 'example-key-for-theme-verification', status: 'active', quota: 100, quota_used: 20, group_id: 1, group, created_at: user.created_at, updated_at: user.updated_at }] }
  if (path === '/admin/users') return { ...empty, total: 1, items: [{ ...user, notes: '', group_rates: {} }] }
  if (path === '/admin/groups') return { ...empty, total: 1, items: [group] }
  if (path === '/admin/groups/all' || path === '/groups/available') return [group]
  if (path === '/setup/status') return { needs_setup: true }
  if (path === '/openclaw/career/profile') return profile
  if (path === '/openclaw/career/schedules') return { items: [] }
  if (path === '/openclaw/lifecycle') return { can_create: true, self_service_enabled: true, hub_name: 'Claw Hub I' }
  if (/\/openclaw\/(instances|catalog|access-requests)$/.test(path)) return empty
  if (path === '/admin/openclaw/summary') return { total: 0, running: 0, pending: 0, failed: 0 }
  if (path.includes('/dashboard/') || path.startsWith('/usage/')) return {
    ...stats, stats, trend: [], models: [], groups: [], ranking: [], items: [], by_platform: [],
    stats_updated_at: '2026-01-01T09:00:00Z', start_date: '2026-01-01', end_date: '2026-01-02', granularity: 'day',
  }
  if (path === '/payment/checkout-info') return { config: { enabled: true, payment_methods: [], payment_types: [] }, plans: [], limits: {}, subscriptions: [] }
  if (path === '/payment/config' || path === '/admin/payment/config') return { enabled: true, payment_methods: [], payment_types: [] }
  if (path === '/subscriptions/summary') return { active_count: 0, subscriptions: [] }
  if (path === '/admin/plugins') return []
  if (path === '/admin/payment/dashboard') return { today_amount: {}, total_amount: {}, avg_amount: {}, today_count: 0, total_count: 0, daily_series: [], payment_methods: [], top_users: {} }
  if (path === '/admin/prompt-audit/config') return { enabled: true, blocking_enabled: false, blocking_latest_turn_only: false, store_pass_events: false, effective_mode: 'async_audit', strategy: 'priority', worker_count: 4, queue_capacity: 100, scanners: [], all_groups: true, group_ids: [], endpoints: [], config_version: 1, updated_at: user.updated_at, updated_by: user.id, change_summary: '{}' }
  if (path === '/admin/prompt-audit/runtime') return { process_status: 'running', effective_mode: 'async_audit', expected_config_version: 1, active_config_version: 1, worker_total: 4, worker_active: 0, queue_capacity: 100, queue: { staging: 0, queued: 0, processing: 0, retry: 0, done: 0, failed: 0, active: 0 }, processed_total: 0, failed_total: 0, enqueued_total: 0, dropped_total: 0, database_status: 'ok', redis_status: 'ok', endpoints: {}, guard_metrics: { total: 0, allowed: 0, flagged: 0, blocked: 0, unavailable: 0, invalid: 0, timeouts: 0, failovers: 0, bulkhead_full: 0, record_failed: 0 } }
  if (path === '/user/auth-bindings' || path === '/user/account-bindings') return { providers: [], bindings: [] }
  if (path.includes('/passkeys') || path.includes('/user-attributes')) return []
  if (path.includes('/totp/status')) return { enabled: false }
  if (path.includes('/affiliate') || path === '/user/aff') return { ...empty, enabled: true, balance: 0, total_commission: 0, total_referred: 0, commission_rate: 0.1, aff_count: 0, aff_quota: 0, aff_frozen_quota: 0, aff_history_quota: 0, aff_code: 'EXAMPLE', invitees: [], effective_rebate_rate_percent: 10 }
  if (path === '/model-plaza') return { models: [], groups: [], platforms: [], generated_at: '2026-01-01T09:00:00Z' }
  if (path.endsWith('/channel-monitor-v2/config')) return monitorConfig
  if (path.includes('/channel-monitor')) return {
    config: monitorConfig, coverage, metrics: { request_count: 0, success_requests: 0, error_requests: 0, token_count: 0, rpm: 0, tpm: 0, error_rate: 0, cache_rate: 0, cache_rate_numerator: 0, cache_rate_denominator: 0, ttft: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null }, duration: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null } },
    health: { overall: 'unknown', minimum_sample: 10, error_rate: 'unknown', ttft: 'unknown' },
    platforms: [], models: [], groups: [], users: [], trend: [], summary: {}, items: [], updated_at: '2026-01-01T09:00:00Z',
  }
  if (/\/(announcements|active|all|plans|platforms|providers|custom-menu-items|available|groups|models)$/.test(path) && !['/admin/groups', '/admin/announcements'].includes(path)) return []
  return empty
}

// All API traffic is intercepted, including writes. No fixture test can reach a real account.
export async function prepare(page: Page, theme: 'light' | 'dark', authenticated = true, role: 'user' | 'admin' = 'user') {
  const currentUser = { ...user, role }
  await page.addInitScript(({ theme, authenticated, user, settings }) => {
    if (!sessionStorage.getItem('theme-audit-seeded')) {
      localStorage.setItem('theme', theme)
      sessionStorage.setItem('theme-audit-seeded', 'true')
    }
    localStorage.setItem('locale', 'en')
    localStorage.setItem(`admin_guide_${user.id}_admin_v4_interactive`, 'true')
    localStorage.setItem(`user_guide_${user.id}_user_v4_interactive`, 'true')
    if (authenticated) {
      localStorage.setItem('auth_token', 'theme-fixture-token')
      localStorage.setItem('auth_user', JSON.stringify(user))
    }
    Object.defineProperty(window, '__APP_CONFIG__', { get: () => settings, set: () => {} })
  }, { theme, authenticated, user: currentUser, settings })
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, '')
    if (path === '/pages/theme-example') {
      await route.fulfill({ contentType: 'text/markdown', body: '# Example guide\n\nReadable custom content.\n\n```text\nexample code\n```' })
      return
    }
    await route.fulfill({ json: { code: 0, data: path === '/auth/me' || path === '/user/profile' ? currentUser : response(path) } })
  })
  await page.route(/\/setup\/(status|test-db|test-redis|install)(\?|$)/, route => route.fulfill({
    json: { code: 0, data: { needs_setup: new URL(page.url()).pathname === '/setup', step: 'database' } },
  }))
  // The dev server proxies /setup; serve its SPA document locally for this route.
  await page.route('**/setup', async route => {
    const document = await route.fetch({ url: new URL('/index.html', route.request().url()).href })
    await route.fulfill({ response: document })
  })
}
