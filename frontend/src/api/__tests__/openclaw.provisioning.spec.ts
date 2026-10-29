import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { createOpenClawInstance, provisionOpenClawInstance, startOpenClawInstance, stopOpenClawInstance, deleteOpenClawInstance } from '../openclaw'

vi.mock('../client', () => ({ apiClient: { post: vi.fn(), delete: vi.fn() } }))

describe('Claw Hub lifecycle request budget', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(apiClient.post).mockResolvedValue({ data: { id: 7 } })
    vi.mocked(apiClient.delete).mockResolvedValue({ data: { id: 7 } })
  })

  it('keeps creation and retry connected beyond the worker deadline', async () => {
    await createOpenClawInstance('Example')
    await provisionOpenClawInstance(7)
    for (const call of vi.mocked(apiClient.post).mock.calls) {
      expect(call[2]?.timeout).toBeGreaterThan(300_000)
    }
  })

  it('allows managed start, graceful stop and removal to finish', async () => {
    await startOpenClawInstance(7)
    await stopOpenClawInstance(7)
    await deleteOpenClawInstance(7)
    for (const call of vi.mocked(apiClient.post).mock.calls) {
      expect(call[2]?.timeout).toBeGreaterThan(300_000)
    }
    expect(vi.mocked(apiClient.delete).mock.calls[0]?.[1]?.timeout).toBeGreaterThan(300_000)
  })
})
