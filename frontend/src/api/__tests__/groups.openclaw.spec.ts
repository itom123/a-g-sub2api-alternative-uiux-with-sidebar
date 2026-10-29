import { describe, expect, it, vi } from 'vitest'
import { getAvailable } from '../groups'
import { apiClient } from '../client'
import { isManagedOpenClawGroup } from '@/utils/openclaw'

vi.mock('../client', () => ({ apiClient: { get: vi.fn() } }))

describe('managed OpenClaw group selection', () => {
  it('excludes both the canonical group and legacy CLAW from ordinary API selection', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: [
      { id: 12, name: 'openai-龙虾' },
      { id: 13, name: ' CLAW ' },
      { id: 8, name: 'openai-通用' }
    ] })
    expect(await getAvailable()).toEqual([{ id: 8, name: 'openai-通用' }])
  })

  it('recognizes managed names consistently in key and administration controls', () => {
    for (const name of ['openai-龙虾', ' OPENAI-龙虾 ', 'claw']) {
      expect(isManagedOpenClawGroup(name)).toBe(true)
    }
    expect(isManagedOpenClawGroup('openai-通用')).toBe(false)
    expect(isManagedOpenClawGroup(undefined)).toBe(false)
  })
})
