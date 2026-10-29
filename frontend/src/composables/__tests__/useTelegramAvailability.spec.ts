import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const getStatus = vi.hoisted(() => vi.fn())
const auth = vi.hoisted(() => ({ isAuthenticated: true }))
vi.mock('@/api/telegram', () => ({ telegramAPI: { getStatus } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))

beforeEach(() => {
  vi.resetModules()
  getStatus.mockReset()
  auth.isAuthenticated = true
})

describe('useTelegramAvailability', () => {
  it('asks the server once and shares the answer', async () => {
    getStatus.mockResolvedValue({ available: true, linked: false })
    const { useTelegramAvailability } = await import('../useTelegramAvailability')
    const sidebar = useTelegramAvailability()
    const page = useTelegramAvailability()
    await flushPromises()
    useTelegramAvailability()
    expect(getStatus).toHaveBeenCalledTimes(1)
    expect(sidebar.telegramAvailable.value).toBe(true)
    expect(page.telegramStatusLoaded.value).toBe(true)
  })

  it('reports the bot as off when the server says so', async () => {
    getStatus.mockResolvedValue({ available: false, linked: false })
    const { useTelegramAvailability } = await import('../useTelegramAvailability')
    const state = useTelegramAvailability()
    await flushPromises()
    expect(state.telegramAvailable.value).toBe(false)
    expect(state.telegramStatusLoaded.value).toBe(true)
  })

  it('treats a reply without the flag as off', async () => {
    getStatus.mockResolvedValue({})
    const { useTelegramAvailability } = await import('../useTelegramAvailability')
    const state = useTelegramAvailability()
    await flushPromises()
    // The sidebar shows items whose flag is undefined, so this must be a real false.
    expect(state.telegramAvailable.value).toBe(false)
  })

  it('retries after a failed check', async () => {
    getStatus.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ available: true, linked: false })
    const { useTelegramAvailability } = await import('../useTelegramAvailability')
    const state = useTelegramAvailability()
    await flushPromises()
    expect(state.telegramAvailable.value).toBe(false)
    expect(state.telegramStatusLoaded.value).toBe(false)
    useTelegramAvailability()
    await flushPromises()
    expect(getStatus).toHaveBeenCalledTimes(2)
    expect(state.telegramAvailable.value).toBe(true)
  })

  it('does not ask while signed out', async () => {
    auth.isAuthenticated = false
    const { useTelegramAvailability } = await import('../useTelegramAvailability')
    expect(useTelegramAvailability().telegramAvailable.value).toBe(false)
    expect(getStatus).not.toHaveBeenCalled()
  })
})
