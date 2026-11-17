<template>
  <AuthLayout>
    <div class="space-y-6">
      <!-- Title -->
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">
          {{ t('auth.createAccount') }}
        </h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{ t('auth.signUpToStart', { siteName }) }}
        </p>
      </div>

      <!-- Registration Disabled Message -->
      <div
        v-if="!registrationEnabled && settingsLoaded"
        class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/50 dark:bg-amber-900/20"
      >
        <div class="flex items-start gap-3">
          <div class="flex-shrink-0">
            <Icon name="exclamationCircle" size="md" class="text-amber-500" />
          </div>
          <p class="text-sm text-amber-700 dark:text-amber-400">
            {{ t('auth.registrationDisabled') }}
          </p>
        </div>
      </div>

      <!-- Registration Form -->
      <form v-else @submit.prevent="handleRegister" class="space-y-5">
        <!-- Email Input -->
        <div>
          <label for="email" class="input-label">
            {{ t('auth.emailLabel') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="mail" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="email"
              v-model="formData.email"
              type="email"
              required
              autofocus
              autocomplete="email"
              :disabled="registrationActionDisabled"
              class="input pl-11"
              :class="{ 'input-error': errors.email }"
              :placeholder="t('auth.emailPlaceholder')"
            />
          </div>
        </div>

        <!-- Password Input -->
        <div>
          <label for="password" class="input-label">
            {{ t('auth.passwordLabel') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="lock" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="password"
              v-model="formData.password"
              :type="showPassword ? 'text' : 'password'"
              required
              autocomplete="new-password"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-11"
              :class="{ 'input-error': errors.password }"
              :placeholder="t('auth.createPasswordPlaceholder')"
            />
            <button
              type="button"
              :disabled="registrationActionDisabled"
              @click="showPassword = !showPassword"
              class="absolute inset-y-0 right-0 flex items-center pr-3.5 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-dark-300"
            >
              <Icon v-if="showPassword" name="eyeOff" size="md" />
              <Icon v-else name="eye" size="md" />
            </button>
          </div>
          <p class="input-hint">
            {{ t('auth.passwordHint') }}
          </p>
        </div>

        <!-- Confirm Password Input -->
        <div>
          <label for="confirmPassword" class="input-label">
            {{ t('auth.confirmPassword') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="lock" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="confirmPassword"
              v-model="confirmPassword"
              :type="showConfirmPassword ? 'text' : 'password'"
              required
              autocomplete="new-password"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-11"
              :class="{ 'input-error': errors.confirmPassword }"
              :placeholder="t('auth.confirmPasswordPlaceholder')"
            />
            <button
              type="button"
              :disabled="registrationActionDisabled"
              @click="showConfirmPassword = !showConfirmPassword"
              class="absolute inset-y-0 right-0 flex items-center pr-3.5 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-dark-300"
            >
              <Icon v-if="showConfirmPassword" name="eyeOff" size="md" />
              <Icon v-else name="eye" size="md" />
            </button>
          </div>
        </div>

        <!-- Invitation Code Input (Required when enabled) -->
        <div v-if="invitationCodeEnabled">
          <label for="invitation_code" class="input-label">
            {{ t('auth.invitationCodeLabel') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="key" size="md" :class="invitationValidation.valid ? 'text-green-500' : 'text-gray-400 dark:text-dark-500'" />
            </div>
            <input
              id="invitation_code"
              v-model="formData.invitation_code"
              type="text"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-10"
              :class="{
                'border-green-500 focus:border-green-500 focus:ring-green-500': invitationValidation.valid,
                'border-red-500 focus:border-red-500 focus:ring-red-500': invitationValidation.invalid || errors.invitation_code
              }"
              :placeholder="t('auth.invitationCodePlaceholder')"
              @input="handleInvitationCodeInput"
            />
            <!-- Validation indicator -->
            <div v-if="invitationValidating" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <svg class="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div v-else-if="invitationValidation.valid" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="checkCircle" size="md" class="text-green-500" />
            </div>
            <div v-else-if="invitationValidation.invalid || errors.invitation_code" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="exclamationCircle" size="md" class="text-red-500" />
            </div>
          </div>
          <!-- Invitation code validation result -->
          <transition name="fade">
            <div v-if="invitationValidation.valid" class="mt-2 flex items-center gap-2 rounded-lg bg-green-50 px-3 py-2 dark:bg-green-900/20">
              <Icon name="checkCircle" size="sm" class="text-green-600 dark:text-green-400" />
              <span class="text-sm text-green-700 dark:text-green-400">
                {{ t('auth.invitationCodeValid') }}
              </span>
            </div>
          </transition>
          <p
            v-if="invitationValidation.unavailable"
            class="input-hint"
            data-testid="invitation-check-unavailable"
          >
            {{ t('auth.invitationCodeCheckUnavailable') }}
          </p>
          <p v-else-if="affiliateEnabled && !invitationValidation.valid" class="input-hint">
            {{ t('auth.invitationCodeReferralHint') }}
          </p>
        </div>

        <!-- Affiliate Invitation Code Input (Optional) -->
        <div v-else-if="affiliateEnabled" data-testid="affiliate-invitation-field">
          <label for="affiliate_code" class="input-label">
            {{ t('auth.invitationCodeLabel') }}
            <span class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500">({{ t('common.optional') }})</span>
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="key" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="affiliate_code"
              v-model="formData.aff_code"
              type="text"
              :disabled="registrationActionDisabled"
              class="input pl-11"
              :placeholder="t('auth.invitationCodePlaceholder')"
            />
          </div>
        </div>

        <!-- Promo Code Input (Optional) -->
        <div v-if="promoCodeEnabled">
          <label for="promo_code" class="input-label">
            {{ t('auth.promoCodeLabel') }}
            <span class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500">({{ t('common.optional') }})</span>
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="gift" size="md" :class="promoValidation.valid ? 'text-green-500' : 'text-gray-400 dark:text-dark-500'" />
            </div>
            <input
              id="promo_code"
              v-model="formData.promo_code"
              type="text"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-10"
              :class="{
                'border-green-500 focus:border-green-500 focus:ring-green-500': promoValidation.valid,
                'border-red-500 focus:border-red-500 focus:ring-red-500': promoValidation.invalid
              }"
              :placeholder="t('auth.promoCodePlaceholder')"
              @input="handlePromoCodeInput"
            />
            <!-- Validation indicator -->
            <div v-if="promoValidating" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <svg class="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div v-else-if="promoValidation.valid" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="checkCircle" size="md" class="text-green-500" />
            </div>
            <div v-else-if="promoValidation.invalid" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="exclamationCircle" size="md" class="text-red-500" />
            </div>
          </div>
          <!-- Promo code validation result -->
          <transition name="fade">
            <div v-if="promoValidation.valid" class="mt-2 flex items-center gap-2 rounded-lg bg-green-50 px-3 py-2 dark:bg-green-900/20">
              <Icon name="gift" size="sm" class="text-green-600 dark:text-green-400" />
              <span class="text-sm text-green-700 dark:text-green-400">
                {{ t('auth.promoCodeValid', { amount: cnyNumber(promoValidation.bonusAmount) }) }}
              </span>
            </div>
          </transition>
          <p v-if="promoValidation.unavailable" class="input-hint" data-testid="promo-check-unavailable">
            {{ t('auth.promoCodeCheckUnavailable') }}
          </p>
        </div>

        <!-- Turnstile Widget -->
        <div v-if="captchaEnabled" data-testid="registration-turnstile">
          <TurnstileWidget
            ref="turnstileRef"
            :turnstile-enabled="turnstileEnabled"
            :turnstile-site-key="turnstileSiteKey"
            :tencent-enabled="tencentCaptchaEnabled"
            :tencent-app-id="tencentCaptchaAppId"
            :tencent-region="tencentCaptchaRegion"
            :aliyun-enabled="aliyunCaptchaEnabled"
            :aliyun-scene-id="aliyunCaptchaSceneId"
            :aliyun-prefix="aliyunCaptchaPrefix"
            :aliyun-region="aliyunCaptchaRegion"
            @verify="onTurnstileVerify"
            @expire="onTurnstileExpire"
            @error="onTurnstileError"
          />
        </div>

        <!-- Why the last submit stopped; stays until the next attempt -->
        <div
          v-if="errorMessage"
          role="alert"
          data-testid="register-error"
          class="flex items-start gap-2 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400"
        >
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ errorMessage }}</span>
        </div>

        <LoginAgreementPrompt
          v-if="loginAgreementEnabled"
          :accepted="agreementAccepted"
          :documents="loginAgreementDocuments"
          :mode="loginAgreementMode"
          :updated-at="loginAgreementUpdatedAt"
          :visible="showAgreementModal"
          @accept="acceptLoginAgreement"
          @reject="rejectLoginAgreement"
          @open="showAgreementModal = true"
        />

        <!-- Submit Button -->
        <button
          type="submit"
          :disabled="registrationActionDisabled || (turnstileEnabled && !turnstileToken)"
          class="btn btn-primary w-full"
        >
          <svg
            v-if="isLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin text-white"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          <Icon v-else name="userPlus" size="md" class="mr-2" />
          {{
            isLoading
              ? t('auth.processing')
              : emailVerifyEnabled
                ? t('auth.continue')
                : t('auth.createAccount')
          }}
        </button>

      </form>

      <div v-if="showOAuthLogin" class="space-y-3 pt-1">
        <div class="flex items-center gap-3">
          <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('auth.oauthOrContinue') }}
          </span>
          <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
        </div>

        <EmailOAuthButtons
          :disabled="registrationActionDisabled"
          :aff-code="formData.aff_code"
          :promo-code="formData.promo_code"
          :github-enabled="githubOAuthEnabled"
          :google-enabled="googleOAuthEnabled"
          :show-divider="false"
          @start="handleOAuthStart"
        />

        <LinuxDoOAuthSection
          v-if="linuxdoOAuthEnabled"
          :disabled="registrationActionDisabled"
          :aff-code="formData.aff_code"
          :promo-code="formData.promo_code"
          :show-divider="false"
          @start="handleOAuthStart"
        />
        <WechatOAuthSection
          v-if="wechatOAuthEnabled"
          :disabled="registrationActionDisabled"
          :aff-code="formData.aff_code"
          :show-divider="false"
          @start="handleOAuthStart"
        />
        <OidcOAuthSection
          v-if="oidcOAuthEnabled"
          :disabled="registrationActionDisabled"
          :provider-name="oidcOAuthProviderName"
          :aff-code="formData.aff_code"
          :show-divider="false"
          @start="handleOAuthStart"
        />
      </div>
    </div>

    <!-- Footer -->
    <template #footer>
      <p class="text-gray-500 dark:text-dark-400">
        {{ t('auth.alreadyHaveAccount') }}
        <router-link
          to="/login"
          class="font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
        >
          {{ t('auth.signIn') }}
        </router-link>
      </p>
    </template>
  </AuthLayout>
</template>

<script setup lang="ts">
import { cnyNumber } from '@/utils/currency'
import { computed, ref, reactive, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import LinuxDoOAuthSection from '@/components/auth/LinuxDoOAuthSection.vue'
import OidcOAuthSection from '@/components/auth/OidcOAuthSection.vue'
import WechatOAuthSection from '@/components/auth/WechatOAuthSection.vue'
import EmailOAuthButtons from '@/components/auth/EmailOAuthButtons.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import { useAuthStore, useAppStore } from '@/stores'
import {
  buildOAuthLoginStartURL,
  getPublicSettings,
  isWeChatWebOAuthEnabled,
  startOAuthLogin,
  type OAuthLoginStart,
  validatePromoCode,
  validateInvitationCode
} from '@/api/auth'
import { buildAuthErrorMessage } from '@/utils/authError'
import { extractApiErrorCode, extractI18nErrorMessage } from '@/utils/apiError'
import { DEFAULT_SITE_NAME } from '@/utils/branding'
import {
  formatRegistrationEmailSuffixWhitelistForMessage,
  isRegistrationEmailSuffixAllowed,
  normalizeRegistrationEmailSuffixWhitelist
} from '@/utils/registrationEmailPolicy'
import {
  clearAffiliateReferralCode,
  loadAffiliateReferralCode,
  resolveAffiliateReferralCode
} from '@/utils/oauthAffiliate'
import type { LoginAgreementDocument } from '@/types'

const { t, locale } = useI18n()
const LOGIN_AGREEMENT_STORAGE_KEY = 'sub2api_login_agreement_consent'

// ==================== Router & Stores ====================

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const settingsLoaded = ref<boolean>(false)
const errorMessage = ref<string>('')
const showPassword = ref<boolean>(false)
const showConfirmPassword = ref<boolean>(false)
const confirmPassword = ref('')

// Public settings
const registrationEnabled = ref<boolean>(true)
const emailVerifyEnabled = ref<boolean>(false)
// Public settings are injected into the app store before Vue mounts. Use that
// value for the first render so a disabled promo-code field never flashes
// while the async settings request is still in flight. If injection is
// unavailable, fail closed until the request explicitly enables the field.
const promoCodeEnabled = ref<boolean>(
  appStore.cachedPublicSettings?.promo_code_enabled === true
)
const invitationCodeEnabled = ref<boolean>(false)
const affiliateEnabled = ref<boolean>(false)
const turnstileEnabled = ref<boolean>(false)
const turnstileSiteKey = ref<string>('')
const tencentCaptchaEnabled = ref<boolean>(false)
const tencentCaptchaAppId = ref<string>('')
const tencentCaptchaRegion = ref<string>('cn')
const aliyunCaptchaEnabled = ref<boolean>(false)
const aliyunCaptchaSceneId = ref<string>('')
const aliyunCaptchaPrefix = ref<string>('')
const aliyunCaptchaRegion = ref<string>('cn')
const siteName = ref<string>(DEFAULT_SITE_NAME)
const linuxdoOAuthEnabled = ref<boolean>(false)
const wechatOAuthEnabled = ref<boolean>(false)
const oidcOAuthEnabled = ref<boolean>(false)
const oidcOAuthProviderName = ref<string>('OIDC')
const githubOAuthEnabled = ref<boolean>(false)
const googleOAuthEnabled = ref<boolean>(false)
const registrationEmailSuffixWhitelist = ref<string[]>([])
// 域名限量注册开关：开启时非白名单域名可注册 1 个账户（由后端判定），前端不做白名单预检。
const emailDomainQuotaEnabled = ref<boolean>(false)
const loginAgreementEnabled = ref<boolean>(false)
const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const loginAgreementUpdatedAt = ref<string>('')
const loginAgreementRevision = ref<string>('')
const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
const agreementAccepted = ref<boolean>(false)
const showAgreementModal = ref<boolean>(false)

// Turnstile
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const turnstileToken = ref<string>('')
const tencentCaptchaRandstr = ref<string>('')
const aliyunCaptchaReady = computed(
  () =>
    aliyunCaptchaEnabled.value &&
    Boolean(aliyunCaptchaSceneId.value) &&
    Boolean(aliyunCaptchaPrefix.value)
)
// 动作触发式验证码（腾讯/阿里云）：提交、OAuth 启动时弹窗验证
const actionCaptchaEnabled = computed(
  () =>
    (tencentCaptchaEnabled.value && Boolean(tencentCaptchaAppId.value)) ||
    aliyunCaptchaReady.value
)
const captchaEnabled = computed(
  () =>
    (turnstileEnabled.value && Boolean(turnstileSiteKey.value)) || actionCaptchaEnabled.value
)

// Runs code checks so that only the latest one counts: a slower response for an
// older value never overwrites the result for what is in the field now.
function createLatestCheck() {
  let seq = 0
  let current: Promise<void> | null = null
  return {
    cancel(): void {
      seq++
      current = null
    },
    run(task: (isCurrent: () => boolean) => Promise<void>): Promise<void> {
      const id = ++seq
      const isCurrent = () => id === seq
      const check = task(isCurrent).finally(() => {
        if (isCurrent()) current = null
      })
      current = check
      return check
    },
    pending(): Promise<void> | null {
      return current
    }
  }
}

// Promo code validation. `unavailable` means the check itself could not run
// (rate limit, network, server error); it says nothing about the code.
const promoValidating = ref<boolean>(false)
const promoValidation = reactive({
  valid: false,
  invalid: false,
  unavailable: false,
  bonusAmount: null as number | null,
  message: ''
})
let promoValidateTimeout: ReturnType<typeof setTimeout> | null = null
const promoCheck = createLatestCheck()
let promoCheckedCode = ''

// Invitation code validation (same states as the promo code)
const invitationValidating = ref<boolean>(false)
const invitationValidation = reactive({
  valid: false,
  invalid: false,
  unavailable: false,
  message: ''
})
let invitationValidateTimeout: ReturnType<typeof setTimeout> | null = null
const invitationCheck = createLatestCheck()
let invitationCheckedCode = ''

const formData = reactive({
  email: '',
  password: '',
  promo_code: '',
  invitation_code: '',
  aff_code: ''
})

const errors = reactive({
  email: '',
  password: '',
  confirmPassword: '',
  turnstile: '',
  invitation_code: ''
})

const validationToastMessage = computed(() =>
  errors.email ||
  errors.password ||
  errors.confirmPassword ||
  (invitationValidation.invalid ? invitationValidation.message : '') ||
  errors.invitation_code ||
  (promoValidation.invalid ? promoValidation.message : '') ||
  errors.turnstile ||
  ''
)

const showOAuthLogin = computed(
  () =>
    linuxdoOAuthEnabled.value ||
    wechatOAuthEnabled.value ||
    oidcOAuthEnabled.value ||
    githubOAuthEnabled.value ||
    googleOAuthEnabled.value
)

const agreementGateActive = computed(
  () => loginAgreementEnabled.value && !agreementAccepted.value
)

const registrationActionDisabled = computed(
  () => isLoading.value || !settingsLoaded.value || agreementGateActive.value
)

watch(validationToastMessage, (value, previousValue) => {
  if (value && value !== previousValue) {
    appStore.showError(value)
  }
})

function syncAffiliateReferralCode(): string {
  const code = resolveAffiliateReferralCode(route.query.aff, route.query.aff_code)
  if (code) {
    formData.aff_code = code
  }
  return code
}

// Invitation-only mode also accepts an inviter's affiliate code, so the code
// from an invite link (/register?aff=CODE) fills the required invitation field.
function prefillInvitationFromReferral(code: string): void {
  if (!code || !invitationCodeEnabled.value || !affiliateEnabled.value || formData.invitation_code.trim()) {
    return
  }
  formData.invitation_code = code
  void validateInvitationCodeDebounced(code)
}

// ==================== Lifecycle ====================

onMounted(async () => {
  syncAffiliateReferralCode()

  try {
    const settings = await getPublicSettings()
    registrationEnabled.value = settings.registration_enabled
    emailVerifyEnabled.value = settings.email_verify_enabled
    promoCodeEnabled.value = settings.promo_code_enabled
    invitationCodeEnabled.value = settings.invitation_code_enabled
    affiliateEnabled.value = settings.affiliate_enabled
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    tencentCaptchaEnabled.value = settings.tencent_captcha_enabled === true
    tencentCaptchaAppId.value = settings.tencent_captcha_app_id || ''
    tencentCaptchaRegion.value = settings.tencent_captcha_region || 'cn'
    aliyunCaptchaEnabled.value = settings.aliyun_captcha_enabled === true
    aliyunCaptchaSceneId.value = settings.aliyun_captcha_scene_id || ''
    aliyunCaptchaPrefix.value = settings.aliyun_captcha_prefix || ''
    aliyunCaptchaRegion.value = settings.aliyun_captcha_region || 'cn'
    siteName.value = settings.site_name || DEFAULT_SITE_NAME
    linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled
    wechatOAuthEnabled.value = isWeChatWebOAuthEnabled(settings)
    oidcOAuthEnabled.value = settings.oidc_oauth_enabled
    oidcOAuthProviderName.value = settings.oidc_oauth_provider_name || 'OIDC'
    githubOAuthEnabled.value = settings.github_oauth_enabled
    googleOAuthEnabled.value = settings.google_oauth_enabled
    registrationEmailSuffixWhitelist.value = normalizeRegistrationEmailSuffixWhitelist(
      settings.registration_email_suffix_whitelist || []
    )
    emailDomainQuotaEnabled.value = settings.registration_email_domain_quota_enabled === true
    applyLoginAgreementSettings(settings)

    // Read promo code from URL parameter only if promo code is enabled
    if (promoCodeEnabled.value) {
      const promoParam = route.query.promo as string
      if (promoParam) {
        formData.promo_code = promoParam
        // Validate the promo code from URL
        await validatePromoCodeDebounced(promoParam)
      }
    }
    prefillInvitationFromReferral(syncAffiliateReferralCode())
  } catch (error) {
    console.error('Failed to load public settings:', error)
    loginAgreementEnabled.value = false
    agreementAccepted.value = true
  } finally {
    settingsLoaded.value = true
  }
})

watch(
  () => [route.query.aff, route.query.aff_code],
  () => {
    prefillInvitationFromReferral(syncAffiliateReferralCode())
  }
)

onUnmounted(() => {
  if (promoValidateTimeout) {
    clearTimeout(promoValidateTimeout)
  }
  if (invitationValidateTimeout) {
    clearTimeout(invitationValidateTimeout)
  }
})

// ==================== Login Agreement ====================

function applyLoginAgreementSettings(settings: {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}): void {
  const documents = Array.isArray(settings.login_agreement_documents)
    ? settings.login_agreement_documents.filter((doc) => doc.title?.trim())
    : []
  loginAgreementDocuments.value = documents
  loginAgreementEnabled.value = settings.login_agreement_enabled === true && documents.length > 0
  loginAgreementMode.value = settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal'
  loginAgreementUpdatedAt.value = settings.login_agreement_updated_at || ''
  loginAgreementRevision.value =
    settings.login_agreement_revision ||
    `${loginAgreementUpdatedAt.value}:${documents.map((doc) => `${doc.id}:${doc.title}`).join('|')}`

  agreementAccepted.value = !loginAgreementEnabled.value || hasAcceptedLoginAgreement(loginAgreementRevision.value)
  showAgreementModal.value =
    loginAgreementEnabled.value && !agreementAccepted.value && loginAgreementMode.value !== 'checkbox'
}

function hasAcceptedLoginAgreement(revision: string): boolean {
  if (!revision) {
    return false
  }
  try {
    const raw = localStorage.getItem(LOGIN_AGREEMENT_STORAGE_KEY)
    if (!raw) {
      return false
    }
    const parsed = JSON.parse(raw) as { revision?: string }
    return parsed.revision === revision
  } catch {
    return false
  }
}

function acceptLoginAgreement(): void {
  if (loginAgreementRevision.value) {
    localStorage.setItem(
      LOGIN_AGREEMENT_STORAGE_KEY,
      JSON.stringify({
        revision: loginAgreementRevision.value,
        accepted_at: new Date().toISOString()
      })
    )
  }
  agreementAccepted.value = true
  showAgreementModal.value = false
}

function rejectLoginAgreement(): void {
  localStorage.removeItem(LOGIN_AGREEMENT_STORAGE_KEY)
  agreementAccepted.value = false
  showAgreementModal.value = false
  appStore.showWarning(t('legal.loginAgreementPrompt.registerRejectedWarning'))
}

// ==================== Promo Code Validation ====================

function clearPromoValidateTimeout(): void {
  if (promoValidateTimeout) {
    clearTimeout(promoValidateTimeout)
    promoValidateTimeout = null
  }
}

function handlePromoCodeInput(): void {
  const code = formData.promo_code.trim()

  // Clear previous validation and drop any check for the old value
  clearPromoValidateTimeout()
  promoCheck.cancel()
  promoCheckedCode = ''
  promoValidating.value = false
  promoValidation.valid = false
  promoValidation.invalid = false
  promoValidation.unavailable = false
  promoValidation.bonusAmount = null
  promoValidation.message = ''
  errorMessage.value = ''

  if (!code) {
    return
  }

  // Debounce validation
  promoValidateTimeout = setTimeout(() => {
    promoValidateTimeout = null
    void validatePromoCodeDebounced(code)
  }, 500)
}

function validatePromoCodeDebounced(code: string): Promise<void> {
  if (!code.trim()) return Promise.resolve()

  promoValidating.value = true

  return promoCheck.run(async (isCurrent) => {
    try {
      const result = await validatePromoCode(code)
      if (!isCurrent()) return

      promoCheckedCode = code
      promoValidation.unavailable = false
      if (result.valid) {
        promoValidation.valid = true
        promoValidation.invalid = false
        promoValidation.bonusAmount = result.bonus_amount || 0
        promoValidation.message = ''
      } else {
        promoValidation.valid = false
        promoValidation.invalid = true
        promoValidation.bonusAmount = null
        // 根据错误码显示对应的翻译
        promoValidation.message = getPromoErrorMessage(result.error_code)
      }
    } catch (error) {
      if (!isCurrent()) return
      // The check could not run, so the code is neither valid nor invalid yet.
      // Registration ignores a promo code the server rejects, so this never blocks.
      console.error('Failed to validate promo code:', error)
      promoCheckedCode = code
      promoValidation.valid = false
      promoValidation.invalid = false
      promoValidation.unavailable = true
      promoValidation.bonusAmount = null
      promoValidation.message = ''
    } finally {
      if (isCurrent()) promoValidating.value = false
    }
  })
}

// Waits for a promo check still running and checks once more a code that has
// no definite answer yet (never checked, or the last check could not run).
async function settlePromoCheck(code: string): Promise<void> {
  clearPromoValidateTimeout()
  await promoCheck.pending()
  const answered = promoCheckedCode === code && (promoValidation.valid || promoValidation.invalid)
  if (!answered) {
    await validatePromoCodeDebounced(code)
  }
}

function getPromoErrorMessage(errorCode?: string): string {
  switch (errorCode) {
    case 'PROMO_CODE_NOT_FOUND':
      return t('auth.promoCodeNotFound')
    case 'PROMO_CODE_EXPIRED':
      return t('auth.promoCodeExpired')
    case 'PROMO_CODE_DISABLED':
      return t('auth.promoCodeDisabled')
    case 'PROMO_CODE_MAX_USED':
      return t('auth.promoCodeMaxUsed')
    case 'PROMO_CODE_ALREADY_USED':
      return t('auth.promoCodeAlreadyUsed')
    default:
      return t('auth.promoCodeInvalid')
  }
}

// ==================== Invitation Code Validation ====================

function clearInvitationValidateTimeout(): void {
  if (invitationValidateTimeout) {
    clearTimeout(invitationValidateTimeout)
    invitationValidateTimeout = null
  }
}

function handleInvitationCodeInput(): void {
  const code = formData.invitation_code.trim()

  // Clear previous validation and drop any check for the old value
  clearInvitationValidateTimeout()
  invitationCheck.cancel()
  invitationCheckedCode = ''
  invitationValidating.value = false
  invitationValidation.valid = false
  invitationValidation.invalid = false
  invitationValidation.unavailable = false
  invitationValidation.message = ''
  errors.invitation_code = ''
  errorMessage.value = ''

  if (!code) {
    return
  }

  // Debounce validation
  invitationValidateTimeout = setTimeout(() => {
    invitationValidateTimeout = null
    void validateInvitationCodeDebounced(code)
  }, 500)
}

function validateInvitationCodeDebounced(code: string): Promise<void> {
  invitationValidating.value = true

  return invitationCheck.run(async (isCurrent) => {
    try {
      const result = await validateInvitationCode(code)
      if (!isCurrent()) return

      invitationCheckedCode = code
      invitationValidation.unavailable = false
      if (result.valid) {
        invitationValidation.valid = true
        invitationValidation.invalid = false
        invitationValidation.message = ''
      } else {
        invitationValidation.valid = false
        invitationValidation.invalid = true
        invitationValidation.message = getInvitationErrorMessage(result.error_code)
      }
    } catch {
      if (!isCurrent()) return
      // The check could not run (for example the check endpoint's per-IP rate
      // limit, which visitors behind one shared address hit together). That says
      // nothing about the code, so it must not block sign-up: the server checks
      // the code again when the account is created.
      invitationCheckedCode = code
      invitationValidation.valid = false
      invitationValidation.invalid = false
      invitationValidation.unavailable = true
      invitationValidation.message = ''
    } finally {
      if (isCurrent()) invitationValidating.value = false
    }
  })
}

// Waits for an invitation check still running and checks once more a code that
// has no definite answer yet (never checked, or the last check could not run).
async function settleInvitationCheck(code: string): Promise<void> {
  clearInvitationValidateTimeout()
  await invitationCheck.pending()
  const answered =
    invitationCheckedCode === code && (invitationValidation.valid || invitationValidation.invalid)
  if (!answered) {
    await validateInvitationCodeDebounced(code)
  }
}

function getInvitationErrorMessage(errorCode?: string): string {
  switch (errorCode) {
    case 'INVITATION_CODE_NOT_FOUND':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_INVALID':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_USED':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_DISABLED':
      return t('auth.invitationCodeInvalid')
    default:
      return t('auth.invitationCodeInvalid')
  }
}

// ==================== Turnstile Handlers ====================

function onTurnstileVerify(token: string, randstr = ''): void {
  turnstileToken.value = token
  tencentCaptchaRandstr.value = randstr
  errors.turnstile = ''
}

function onTurnstileExpire(): void {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = t('auth.turnstileExpired')
}

function onTurnstileError(): void {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = t('auth.turnstileFailed')
}

function resetCaptchaProof(): void {
  turnstileRef.value?.reset()
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = ''
}

async function acquireActionProof(): Promise<boolean> {
  if (!actionCaptchaEnabled.value) return true

  const proof = await turnstileRef.value?.verifyAction()
  if (!proof) return false

  turnstileToken.value = proof.token
  tencentCaptchaRandstr.value = proof.randstr
  return true
}

async function handleOAuthStart(request: OAuthLoginStart): Promise<void> {
  if (registrationActionDisabled.value) return

  if (!actionCaptchaEnabled.value) {
    window.location.href = buildOAuthLoginStartURL(request)
    return
  }

  isLoading.value = true
  try {
    const proof = await turnstileRef.value?.verifyAction()
    if (!proof) return

    const result = await startOAuthLogin(
      request,
      tencentCaptchaEnabled.value
        ? {
            tencent_captcha_ticket: proof.token,
            tencent_captcha_randstr: proof.randstr
          }
        : { turnstile_token: proof.token }
    )
    window.location.href = result.authorize_url
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(
      error,
      t,
      'auth.errors',
      t('auth.turnstileFailed')
    )
    appStore.showError(errorMessage.value)
  } finally {
    resetCaptchaProof()
    isLoading.value = false
  }
}

// ==================== Validation ====================

function validateEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
  return emailRegex.test(email)
}

function buildEmailSuffixNotAllowedMessage(): string {
  const normalizedWhitelist = normalizeRegistrationEmailSuffixWhitelist(
    registrationEmailSuffixWhitelist.value
  )
  if (normalizedWhitelist.length === 0) {
    return t('auth.emailSuffixNotAllowed')
  }
  const separator = String(locale.value || '').toLowerCase().startsWith('zh') ? '、' : ', '
  return t('auth.emailSuffixNotAllowedWithAllowed', {
    suffixes: formatRegistrationEmailSuffixWhitelistForMessage(normalizedWhitelist, {
      separator,
      more: (count) => t('auth.emailSuffixAllowedMore', { count })
    })
  })
}

function validateForm(): boolean {
  // Reset errors
  errors.email = ''
  errors.password = ''
  errors.confirmPassword = ''
  errors.turnstile = ''
  errors.invitation_code = ''

  let isValid = true

  if (agreementGateActive.value) {
    appStore.showWarning(t('legal.loginAgreementPrompt.registerRequiredWarning'))
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return false
  }

  // Email validation
  if (!formData.email.trim()) {
    errors.email = t('auth.emailRequired')
    isValid = false
  } else if (!validateEmail(formData.email)) {
    errors.email = t('auth.invalidEmail')
    isValid = false
  } else if (
    !emailDomainQuotaEnabled.value &&
    !isRegistrationEmailSuffixAllowed(formData.email, registrationEmailSuffixWhitelist.value)
  ) {
    // 域名限量注册关闭时保持严格白名单预检；开启时交给后端按域名额度判定
    errors.email = buildEmailSuffixNotAllowedMessage()
    isValid = false
  }

  // Password validation
  if (!formData.password) {
    errors.password = t('auth.passwordRequired')
    isValid = false
  } else if (formData.password.length < 6) {
    errors.password = t('auth.passwordMinLength')
    isValid = false
  }

  // Confirm password validation
  if (!confirmPassword.value) {
    errors.confirmPassword = t('auth.confirmPasswordRequired')
    isValid = false
  } else if (formData.password !== confirmPassword.value) {
    errors.confirmPassword = t('auth.passwordsDoNotMatch')
    isValid = false
  }

  // Invitation code validation (required when enabled)
  if (invitationCodeEnabled.value) {
    if (!formData.invitation_code.trim()) {
      errors.invitation_code = t('auth.invitationCodeRequired')
      isValid = false
    }
  }

  // Turnstile validation
  if (turnstileEnabled.value && !turnstileToken.value) {
    errors.turnstile = t('auth.completeVerification')
    isValid = false
  }

  return isValid
}

// ==================== Form Handlers ====================

// Shows why a submit stopped: inline until the next attempt, and as a toast.
// The validation watcher already toasts a message that has just appeared in
// validationToastMessage, so the toast is skipped then; a click must never end
// with nothing on screen, but should not raise the same toast twice either.
function stopSubmit(message: string, toastBefore: string): void {
  if (!message) return
  errorMessage.value = message
  const watcherToasts = message === validationToastMessage.value && message !== toastBefore
  if (!watcherToasts) {
    appStore.showError(message)
  }
}

// Waits for promo and invitation checks still in flight (or still debounced)
// instead of giving up on the click. Only a definite "invalid" answer stops the
// submit; when a check could not run, the server decides at registration.
async function codesAllowSubmit(): Promise<boolean> {
  const promoCode = formData.promo_code.trim()
  if (promoCode) {
    const toastBefore = validationToastMessage.value
    await settlePromoCheck(promoCode)
    if (promoValidation.invalid) {
      stopSubmit(promoValidation.message || t('auth.promoCodeInvalidCannotRegister'), toastBefore)
      return false
    }
  }

  const invitationCode = formData.invitation_code.trim()
  if (invitationCodeEnabled.value && invitationCode) {
    const toastBefore = validationToastMessage.value
    await settleInvitationCheck(invitationCode)
    if (invitationValidation.invalid) {
      stopSubmit(
        invitationValidation.message || t('auth.invitationCodeInvalidCannotRegister'),
        toastBefore
      )
      return false
    }
  }

  return true
}

async function handleRegister(): Promise<void> {
  if (isLoading.value) return

  // Clear previous error
  errorMessage.value = ''

  // Validate form
  const toastBeforeValidation = validationToastMessage.value
  if (!validateForm()) {
    // The agreement gate explains itself with its own warning and modal.
    if (!agreementGateActive.value) {
      stopSubmit(validationToastMessage.value, toastBeforeValidation)
    }
    return
  }

  // Busy from here on, so a second click cannot start a parallel submit while
  // the code checks or the captcha are still pending.
  isLoading.value = true
  try {
    if (!(await codesAllowSubmit())) {
      return
    }
    if (!(await acquireActionProof())) {
      return
    }
    await submitRegistration()
  } finally {
    isLoading.value = false
  }
}

async function submitRegistration(): Promise<void> {
  try {
    const affCode = formData.aff_code.trim() || loadAffiliateReferralCode()
    if (affCode) {
      formData.aff_code = affCode
    }

    // If email verification is enabled, redirect to verification page
    if (emailVerifyEnabled.value) {
      // Store registration data in sessionStorage
      sessionStorage.setItem(
        'register_data',
        JSON.stringify({
          email: formData.email,
          password: formData.password,
          turnstile_token:
            turnstileEnabled.value || aliyunCaptchaEnabled.value ? turnstileToken.value : undefined,
          tencent_captcha_ticket: tencentCaptchaEnabled.value ? turnstileToken.value : undefined,
          tencent_captcha_randstr: tencentCaptchaEnabled.value ? tencentCaptchaRandstr.value : undefined,
          promo_code: formData.promo_code || undefined,
          invitation_code: formData.invitation_code || undefined,
          ...(affCode ? { aff_code: affCode } : {})
        })
      )

      // Navigate to email verification page
      await router.push('/email-verify')
      return
    }

    // Otherwise, directly register
    await authStore.register({
      email: formData.email,
      password: formData.password,
      turnstile_token:
        turnstileEnabled.value || aliyunCaptchaEnabled.value ? turnstileToken.value : undefined,
      tencent_captcha_ticket: tencentCaptchaEnabled.value ? turnstileToken.value : undefined,
      tencent_captcha_randstr: tencentCaptchaEnabled.value ? tencentCaptchaRandstr.value : undefined,
      promo_code: formData.promo_code || undefined,
      invitation_code: formData.invitation_code || undefined,
      ...(affCode ? { aff_code: affCode } : {})
    })
    clearAffiliateReferralCode()

    // Show success toast
    appStore.showSuccess(t('auth.accountCreatedSuccess', { siteName: siteName.value }))

    // Redirect to dashboard
    await router.push('/dashboard')
  } catch (error: unknown) {
    // Handle registration error
    errorMessage.value = buildRegistrationErrorMessage(error, t('auth.registrationFailed'))

    // Also show error toast
    appStore.showError(errorMessage.value)
  } finally {
    if (captchaEnabled.value) {
      resetCaptchaProof()
    }
  }
}

function buildRegistrationErrorMessage(error: unknown, fallback: string): string {
  switch (extractApiErrorCode(error)) {
    case 'EMAIL_DOMAIN_REGISTRATION_LIMIT':
      return t('auth.emailDomainRegistrationLimit')
    case 'EMAIL_BLOCKED':
      return t('auth.errors.EMAIL_BLOCKED')
    case 'INVITATION_CODE_REQUIRED':
      return t('auth.invitationCodeRequired')
    case 'INVITATION_CODE_INVALID':
      return t('auth.invitationCodeInvalid')
  }
  return buildAuthErrorMessage(error, { fallback })
}
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
