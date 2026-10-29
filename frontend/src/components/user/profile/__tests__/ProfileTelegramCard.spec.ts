import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileTelegramCard from '@/components/user/profile/ProfileTelegramCard.vue'

const { getStatusMock, resetBindCodeMock, unlinkMock, showSuccessMock, showInfoMock, showErrorMock } = vi.hoisted(() => ({
  getStatusMock: vi.fn(),
  resetBindCodeMock: vi.fn(),
  unlinkMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showInfoMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/api/telegram', () => ({
  telegramAPI: { getStatus: getStatusMock, resetBindCode: resetBindCodeMock, unlink: unlinkMock }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: showSuccessMock, showInfo: showInfoMock, showError: showErrorMock })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => (params?.username ? `${key}:${params.username}` : key)
    })
  }
})

const ConfirmDialogStub = {
  props: ['show'],
  emits: ['confirm', 'cancel'],
  template: '<button v-if="show" data-testid="confirm" @click="$emit(\'confirm\')" />'
}

const mountCard = () => mount(ProfileTelegramCard, { global: { stubs: { ConfirmDialog: ConfirmDialogStub } } })

describe('ProfileTelegramCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('stays hidden when the bot is not available', async () => {
    getStatusMock.mockResolvedValue({ available: false, linked: false })
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.find('[data-testid="profile-telegram-card"]').exists()).toBe(false)
  })

  const findButton = (wrapper: ReturnType<typeof mountCard>, label: string) =>
    wrapper.findAll('button').find((b) => b.text() === label)!

  it('shows the permanent bind command and confirms the link', async () => {
    getStatusMock.mockResolvedValueOnce({ available: true, linked: false, bot_username: 'site_bot', bind_code: 'abc123' })
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.get('[data-testid="telegram-bind-command"]').text()).toBe('/bind abc123')
    expect(wrapper.find('a[href="https://t.me/site_bot?start=abc123"]').exists()).toBe(true)

    getStatusMock.mockResolvedValueOnce({ available: true, linked: true, bot_username: 'site_bot', telegram_username: 'alice' })
    await findButton(wrapper, 'telegramBot.checkLinked').trigger('click')
    await flushPromises()
    expect(showSuccessMock).toHaveBeenCalledWith('telegramBot.linkedSuccess')
    expect(wrapper.text()).toContain('telegramBot.linkedAs:@alice')
  })

  it('reports when the bot has not received the command yet', async () => {
    getStatusMock.mockResolvedValue({ available: true, linked: false, bot_username: 'site_bot', bind_code: 'abc' })
    const wrapper = mountCard()
    await flushPromises()

    await findButton(wrapper, 'telegramBot.checkLinked').trigger('click')
    await flushPromises()
    expect(showInfoMock).toHaveBeenCalledWith('telegramBot.stillPending')
  })

  it('resets the bind code after confirmation', async () => {
    getStatusMock.mockResolvedValue({ available: true, linked: false, bot_username: 'site_bot', bind_code: 'old' })
    resetBindCodeMock.mockResolvedValue({ available: true, linked: false, bot_username: 'site_bot', bind_code: 'new' })
    const wrapper = mountCard()
    await flushPromises()

    await findButton(wrapper, 'telegramBot.resetCode').trigger('click')
    await wrapper.find('[data-testid="confirm"]').trigger('click')
    await flushPromises()
    expect(resetBindCodeMock).toHaveBeenCalled()
    expect(showSuccessMock).toHaveBeenCalledWith('telegramBot.codeReset')
    expect(wrapper.get('[data-testid="telegram-bind-command"]').text()).toBe('/bind new')
  })

  it('unlinks after confirmation', async () => {
    getStatusMock.mockResolvedValueOnce({ available: true, linked: true, bot_username: 'site_bot', telegram_username: 'alice' })
    getStatusMock.mockResolvedValueOnce({ available: true, linked: false, bot_username: 'site_bot' })
    unlinkMock.mockResolvedValue(undefined)
    const wrapper = mountCard()
    await flushPromises()

    await wrapper.find('button.btn-outline-danger').trigger('click')
    await wrapper.find('[data-testid="confirm"]').trigger('click')
    await flushPromises()
    expect(unlinkMock).toHaveBeenCalled()
    expect(wrapper.text()).toContain('telegramBot.notLinked')
  })
})
