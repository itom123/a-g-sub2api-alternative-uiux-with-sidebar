import { beforeEach, describe, expect, it, vi } from 'vitest'
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post } }))
import { usageAPI } from '@/api/usage'
import { getProfile, getMyPlatformQuotas } from '@/api/user'
import { keysAPI } from '@/api/keys'
const range = { start_date: '2026-10-26', end_date: '2026-11-01', timezone: 'UTC' }
beforeEach(() => { get.mockReset(); post.mockReset(); get.mockResolvedValue({ data: {} }); post.mockResolvedValue({ data: {} }) })
describe('usage dashboard v2 API boundary', () => {
  it('uses exact current-user dashboard paths and params', async () => {
    await usageAPI.getStatsByRange(range); await usageAPI.getDashboardStats(); await usageAPI.getDashboardTrend(range); await usageAPI.getDashboardModels(range); await usageAPI.getDashboardSnapshotV2({ ...range, granularity: 'day', include_trend: true }); await usageAPI.listMyErrorRequests({ ...range, page: 1, page_size: 10 }); await getProfile(); await getMyPlatformQuotas(); await keysAPI.list(1, 100)
    const paths = get.mock.calls.map(([path]) => String(path)); expect(paths).toEqual(['/usage/stats', '/usage/dashboard/stats', '/usage/dashboard/trend', '/usage/dashboard/models', '/usage/dashboard/snapshot-v2', '/usage/errors', '/user/profile', '/user/platform-quotas', '/keys']); expect(paths.some(path => path.includes('/admin/'))).toBe(false); expect(JSON.stringify(get.mock.calls)).not.toContain('user_id')
  })
  it('uses daily and batch endpoints without user selector', async () => { await usageAPI.getDashboardApiKeysUsage([1, 2]); await usageAPI.getMyApiKeyDailyUsage(2, 30, 'UTC'); expect(post).toHaveBeenCalledWith('/usage/dashboard/api-keys-usage', { api_key_ids: [1, 2] }, expect.anything()); expect(get).toHaveBeenCalledWith('/user/api-keys/2/usage/daily', expect.objectContaining({ params: { days: 30, timezone: 'UTC' } })); expect(JSON.stringify({ get: get.mock.calls, post: post.mock.calls })).not.toContain('user_id') })
})
