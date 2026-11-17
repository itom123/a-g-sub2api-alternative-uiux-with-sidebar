import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '@/views/auth/RegisterView.vue'

const {
  getPublicSettingsMock,
  registerMock,
  showErrorMock,
  pushMock,
  verifyActionMock,
  validateInvitationCodeMock,
  routeQuery,
  appStoreMock
} = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  registerMock: vi.fn(),
  showErrorMock: vi.fn(),
  pushMock: vi.fn(),
  verifyActionMock: vi.fn(),
  validateInvitationCodeMock: vi.fn(),
  routeQuery: {} as Record<string, string>,
  appStoreMock: {
    cachedPublicSettings: null as { promo_code_enabled?: boolean } | null,
    showError: (...args: unknown[]) => showErrorMock(...args),
    showSuccess: vi.fn(),
    showWarning: vi.fn()
  }
}))

const publicSettings = {
  registration_enabled: true,
  email_verify_enabled: false,
  promo_code_enabled: false,
  invitation_code_enabled: false,
  affiliate_enabled: true,
  turnstile_enabled: true,
  turnstile_site_key: 'site-key',
  site_name: 'Sub2API',
  registration_email_suffix_whitelist: [],
  linuxdo_oauth_enabled: false,
  wechat_oauth_enabled: false,
  oidc_oauth_enabled: false,
  github_oauth_enabled: false,
  google_oauth_enabled: false
}

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock }),
  useRoute: () => ({ query: routeQuery })
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key
    }
  }),
  useI18n: () => ({
    t: (key: string) =>
      key === 'auth.emailDomainRegistrationLimit'
        ? '该邮箱域名无法注册新账户。请使用主流邮箱注册；如需使用企业邮箱，请联系客服添加域名白名单。'
        : key,
    locale: { value: 'en' }
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ register: (...args: unknown[]) => registerMock(...args) }),
  useAppStore: () => appStoreMock
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
    validateInvitationCode: (...args: unknown[]) => validateInvitationCodeMock(...args)
  }
})

function mountRegister() {
  return mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: {
          template: '<div data-testid="turnstile-widget" />',
          methods: { verifyAction: verifyActionMock, reset: vi.fn() }
        },
        LoginAgreementPrompt: true,
        EmailOAuthButtons: true,
        LinuxDoOAuthSection: true,
        WechatOAuthSection: true,
        OidcOAuthSection: true,
        RouterLink: true,
        transition: false
      }
    }
  })
}

describe('RegisterView', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    registerMock.mockReset()
    showErrorMock.mockReset()
    pushMock.mockReset()
    verifyActionMock.mockReset()
    validateInvitationCodeMock.mockReset()
    validateInvitationCodeMock.mockResolvedValue({ valid: true })
    for (const key of Object.keys(routeQuery)) delete routeQuery[key]
    localStorage.removeItem('affiliate_referral_code')
    appStoreMock.cachedPublicSettings = null
    sessionStorage.removeItem('register_data')
    verifyActionMock.mockResolvedValue({ token: 'ticket', randstr: 'randstr' })
    getPublicSettingsMock.mockResolvedValue(publicSettings)
    registerMock.mockResolvedValue({})
  })

  it('does not flash the promo-code field before disabled settings finish loading', async () => {
    let resolveSettings!: (settings: typeof publicSettings) => void
    getPublicSettingsMock.mockReturnValueOnce(
      new Promise<typeof publicSettings>((resolve) => {
        resolveSettings = resolve
      })
    )

    const wrapper = mountRegister()

    expect(wrapper.find('#promo_code').exists()).toBe(false)

    resolveSettings(publicSettings)
    await flushPromises()

    expect(wrapper.find('#promo_code').exists()).toBe(false)
  })

  it('uses injected public settings to show an enabled promo-code field on first render', () => {
    appStoreMock.cachedPublicSettings = { promo_code_enabled: true }
    getPublicSettingsMock.mockReturnValueOnce(new Promise(() => {}))

    const wrapper = mountRegister()

    expect(wrapper.find('#promo_code').exists()).toBe(true)
  })

  it.each([
    ['', 'auth.confirmPasswordRequired'],
    ['different-password', 'auth.passwordsDoNotMatch']
  ])('blocks invalid confirmation %j before captcha and allows correction', async (confirmation, error) => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      tencent_captcha_enabled: true,
      tencent_captcha_app_id: 'app-id'
    })
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue(confirmation)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith(error)
    expect(wrapper.get('#confirmPassword').classes()).toContain('input-error')
    expect(registerMock).not.toHaveBeenCalled()
    expect(verifyActionMock).not.toHaveBeenCalled()
    expect(pushMock).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('register_data')).toBeNull()

    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.get('#confirmPassword').classes()).not.toContain('input-error')
    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(registerMock).toHaveBeenCalledWith({
      email: 'user@example.com',
      password: 'secret-123',
      turnstile_token: undefined,
      tencent_captcha_ticket: 'ticket',
      tencent_captcha_randstr: 'randstr',
      promo_code: undefined,
      invitation_code: undefined
    })
    expect(pushMock).toHaveBeenCalledWith('/dashboard')
  })

  it('requires matching confirmation before storing only the registration fields for email verification', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      email_verify_enabled: true
    })
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('different-password')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(sessionStorage.getItem('register_data')).toBeNull()
    expect(pushMock).not.toHaveBeenCalled()

    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(JSON.parse(sessionStorage.getItem('register_data')!)).toEqual({
      email: 'user@example.com',
      password: 'secret-123'
    })
    expect(pushMock).toHaveBeenCalledWith('/email-verify')
    expect(registerMock).not.toHaveBeenCalled()
  })

  it('keeps the optional affiliate invitation field before Turnstile', async () => {
    const wrapper = mountRegister()
    await flushPromises()

    const invitationField = wrapper.get('[data-testid="affiliate-invitation-field"]')
    const turnstile = wrapper.get('[data-testid="registration-turnstile"]')

    expect(invitationField.get('input').attributes('id')).toBe('affiliate_code')
    expect(invitationField.text()).toContain('common.optional')
    expect(
      invitationField.element.compareDocumentPosition(turnstile.element) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('uses the mandatory invitation field without duplicating the affiliate field', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      invitation_code_enabled: true
    })

    const wrapper = mountRegister()
    await flushPromises()

    expect(wrapper.find('[data-testid="affiliate-invitation-field"]').exists()).toBe(false)
    expect(wrapper.get('#invitation_code').exists()).toBe(true)
  })

  it('fills the required invitation field from an invite link so referred users can register', async () => {
    routeQuery.aff = 'FRIEND42'
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      invitation_code_enabled: true
    })

    const wrapper = mountRegister()
    await flushPromises()

    expect((wrapper.get('#invitation_code').element as HTMLInputElement).value).toBe('FRIEND42')
    expect(validateInvitationCodeMock).toHaveBeenCalledWith('FRIEND42')
    expect(wrapper.text()).not.toContain('auth.invitationCodeReferralHint')
  })

  it('leaves the invitation field empty when the affiliate program is off', async () => {
    routeQuery.aff = 'FRIEND42'
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      invitation_code_enabled: true,
      affiliate_enabled: false
    })

    const wrapper = mountRegister()
    await flushPromises()

    expect((wrapper.get('#invitation_code').element as HTMLInputElement).value).toBe('')
    expect(validateInvitationCodeMock).not.toHaveBeenCalled()
  })

  it('submits a non-whitelist email domain so the backend can enforce its registration quota', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com'],
      registration_email_domain_quota_enabled: true
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('first@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'first@custom.example' })
    )
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('shows the localized registration domain quota message returned by the backend', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com'],
      registration_email_domain_quota_enabled: true
    })
    registerMock.mockRejectedValueOnce({
      reason: 'EMAIL_DOMAIN_REGISTRATION_LIMIT',
      message: 'raw backend message'
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('second@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith(
      '该邮箱域名无法注册新账户。请使用主流邮箱注册；如需使用企业邮箱，请联系客服添加域名白名单。'
    )
  })

  // 域名限量注册开关默认关闭：恢复 PR5423 之前的客户端白名单预检，非白名单域名不发起注册请求。
  it('rejects a non-whitelist email domain locally when the domain quota switch is disabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com']
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('first@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).not.toHaveBeenCalled()
    // 校验失败通过 validationToastMessage watcher 弹 toast
    expect(showErrorMock).toHaveBeenCalledWith('auth.emailSuffixNotAllowedWithAllowed')
    expect(wrapper.get('#email').classes()).toContain('input-error')
  })

  it('still submits whitelisted email domains when the domain quota switch is disabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com']
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@allowed.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'user@allowed.com' })
    )
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  describe('invitation-only sign-up never ignores a click', () => {
    const invitationOnly = {
      ...publicSettings,
      turnstile_enabled: false,
      invitation_code_enabled: true
    }

    async function fillForm(wrapper: ReturnType<typeof mountRegister>, code: string) {
      await wrapper.get('#email').setValue('invitee@example.com')
      await wrapper.get('#password').setValue('secret-123')
      await wrapper.get('#confirmPassword').setValue('secret-123')
      await wrapper.get('#invitation_code').setValue(code)
    }

    async function submit(wrapper: ReturnType<typeof mountRegister>) {
      await wrapper.get('form').trigger('submit.prevent')
      await flushPromises()
    }

    it('submits a code typed just before clicking without waiting for the debounce', async () => {
      getPublicSettingsMock.mockResolvedValueOnce(invitationOnly)
      const wrapper = mountRegister()
      await flushPromises()
      await fillForm(wrapper, 'INVITE-1')
      await submit(wrapper)

      expect(validateInvitationCodeMock).toHaveBeenCalledOnce()
      expect(validateInvitationCodeMock).toHaveBeenCalledWith('INVITE-1')
      expect(registerMock).toHaveBeenCalledWith(
        expect.objectContaining({ email: 'invitee@example.com', invitation_code: 'INVITE-1' })
      )
      expect(pushMock).toHaveBeenCalledWith('/dashboard')
    })

    it('shows why a rejected code stops the submit, on every click', async () => {
      getPublicSettingsMock.mockResolvedValueOnce(invitationOnly)
      validateInvitationCodeMock.mockResolvedValue({ valid: false, error_code: 'INVITATION_CODE_USED' })
      const wrapper = mountRegister()
      await flushPromises()
      await fillForm(wrapper, 'USED-CODE')

      await submit(wrapper)
      expect(wrapper.get('[data-testid="register-error"]').text()).toContain('auth.invitationCodeInvalid')
      expect(showErrorMock).toHaveBeenCalledTimes(1)

      // The same rejected code clicked again: no repeat check, but visible feedback again.
      await submit(wrapper)
      expect(validateInvitationCodeMock).toHaveBeenCalledOnce()
      expect(wrapper.get('[data-testid="register-error"]').text()).toContain('auth.invitationCodeInvalid')
      expect(showErrorMock).toHaveBeenCalledTimes(2)
      expect(registerMock).not.toHaveBeenCalled()

      // Typing a new code clears the stale message.
      await wrapper.get('#invitation_code').setValue('NEW-CODE')
      expect(wrapper.find('[data-testid="register-error"]').exists()).toBe(false)
    })

    it('lets the server decide when the code check is rate limited', async () => {
      getPublicSettingsMock.mockResolvedValueOnce(invitationOnly)
      validateInvitationCodeMock.mockRejectedValue({ status: 429, message: 'Too many requests' })
      const wrapper = mountRegister()
      await flushPromises()
      await fillForm(wrapper, 'FRIEND42')
      await submit(wrapper)

      expect(wrapper.find('[data-testid="invitation-check-unavailable"]').exists()).toBe(true)
      expect(wrapper.get('#invitation_code').classes()).not.toContain('border-red-500')
      expect(registerMock).toHaveBeenCalledWith(
        expect.objectContaining({ invitation_code: 'FRIEND42' })
      )
    })

    it('shows the localized reason when the server still rejects the code', async () => {
      getPublicSettingsMock.mockResolvedValueOnce(invitationOnly)
      validateInvitationCodeMock.mockRejectedValue({ status: 429 })
      registerMock.mockRejectedValueOnce({
        reason: 'INVITATION_CODE_INVALID',
        message: 'invalid or used invitation code'
      })
      const wrapper = mountRegister()
      await flushPromises()
      await fillForm(wrapper, 'BOGUS')
      await submit(wrapper)

      expect(showErrorMock).toHaveBeenCalledWith('auth.invitationCodeInvalid')
      expect(wrapper.get('[data-testid="register-error"]').text()).toContain('auth.invitationCodeInvalid')
      expect(pushMock).not.toHaveBeenCalled()
    })

    it('waits for the check started by typing instead of ignoring the click', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      try {
        getPublicSettingsMock.mockResolvedValueOnce(invitationOnly)
        let answer!: (result: { valid: boolean }) => void
        validateInvitationCodeMock.mockReturnValueOnce(
          new Promise((resolve) => {
            answer = resolve
          })
        )
        const wrapper = mountRegister()
        await flushPromises()
        await fillForm(wrapper, 'SLOW-CODE')
        vi.advanceTimersByTime(500) // the debounced check is now in flight

        await wrapper.get('form').trigger('submit.prevent')
        await flushPromises()
        expect(registerMock).not.toHaveBeenCalled()
        expect((wrapper.get('button[type="submit"]').element as HTMLButtonElement).disabled).toBe(true)

        answer({ valid: true })
        await flushPromises()

        expect(validateInvitationCodeMock).toHaveBeenCalledOnce()
        expect(registerMock).toHaveBeenCalledWith(
          expect.objectContaining({ invitation_code: 'SLOW-CODE' })
        )
      } finally {
        vi.useRealTimers()
      }
    })

    it('ignores a slow answer for a code the user has since replaced', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      try {
        getPublicSettingsMock.mockResolvedValueOnce(invitationOnly)
        let answerOld!: (result: { valid: boolean }) => void
        validateInvitationCodeMock
          .mockReturnValueOnce(
            new Promise((resolve) => {
              answerOld = resolve
            })
          )
          .mockResolvedValueOnce({ valid: false, error_code: 'INVITATION_CODE_NOT_FOUND' })
        const wrapper = mountRegister()
        await flushPromises()
        await fillForm(wrapper, 'OLD-CODE')
        vi.advanceTimersByTime(500)
        await wrapper.get('#invitation_code').setValue('TYPO-CODE')
        vi.advanceTimersByTime(500)
        await flushPromises()

        answerOld({ valid: true })
        await flushPromises()

        expect(wrapper.text()).not.toContain('auth.invitationCodeValid')
        await submit(wrapper)
        expect(registerMock).not.toHaveBeenCalled()
        expect(wrapper.get('[data-testid="register-error"]').text()).toContain('auth.invitationCodeInvalid')
      } finally {
        vi.useRealTimers()
      }
    })
  })
})
