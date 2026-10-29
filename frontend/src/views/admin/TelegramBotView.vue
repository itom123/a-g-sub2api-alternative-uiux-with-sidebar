<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6" data-testid="telegram-bot-view">
      <div v-if="loading" class="flex justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-500"></div>
      </div>

      <template v-else-if="settings">
        <!-- Bot -->
        <div class="card p-6">
          <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
            <div>
              <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.telegramBot.setup.title') }}</h3>
              <p class="mt-1 max-w-2xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.description') }}</p>
            </div>
            <label class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input v-model="form.enabled" type="checkbox" />
              <span>{{ t('admin.telegramBot.setup.enabled') }}</span>
            </label>
          </div>

          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">{{ t('admin.telegramBot.setup.token') }}</label>
              <input
                v-model="form.bot_token"
                type="password"
                autocomplete="off"
                class="input w-full"
                :placeholder="settings.token_configured ? t('admin.telegramBot.setup.tokenConfigured') : t('admin.telegramBot.setup.tokenPlaceholder')"
              />
            </div>
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">{{ t('admin.telegramBot.setup.proxy') }}</label>
              <input v-model="form.proxy_url" class="input w-full" placeholder="socks5://127.0.0.1:1080" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.proxyHint') }}</p>
            </div>
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">{{ t('admin.telegramBot.setup.newsChannel') }}</label>
              <input v-model="form.news_channel_url" class="input w-full" placeholder="https://t.me/yournews" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.newsChannelHint') }}</p>
            </div>
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">{{ t('admin.telegramBot.setup.group') }}</label>
              <input v-model="form.group_invite_url" class="input w-full" placeholder="https://t.me/+AbCdEf123" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.groupHint') }}</p>
            </div>
          </div>

          <dl class="mt-4 grid grid-cols-2 gap-3 text-sm md:grid-cols-4">
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.bot') }}</dt>
              <dd class="mt-1 font-mono text-gray-900 dark:text-white">
                <a v-if="settings.bot_username" :href="`https://t.me/${settings.bot_username}`" target="_blank" rel="noopener noreferrer" class="text-primary-600 hover:underline dark:text-primary-400">@{{ settings.bot_username }}</a>
                <span v-else>-</span>
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.status') }}</dt>
              <dd class="mt-1" :class="statusClass">{{ statusText }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.linkedUsers') }}</dt>
              <dd class="mt-1 text-gray-900 dark:text-white">{{ settings.linked_users }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.linkedAdmins') }}</dt>
              <dd class="mt-1 text-gray-900 dark:text-white">{{ settings.linked_admins }}</dd>
            </div>
          </dl>
          <p v-if="settings.last_error" class="mt-3 rounded bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
            {{ settings.last_error }}
          </p>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.setup.adminHint') }}</p>
        </div>

        <!-- Notifications -->
        <div class="card p-6">
          <h3 class="mb-4 text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.telegramBot.notifications.title') }}</h3>
          <div class="space-y-4">
            <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input v-model="form.notify_balance_low" type="checkbox" class="mt-1" />
              <span>
                {{ t('admin.telegramBot.notifications.balanceLow') }}
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.notifications.balanceLowHint') }}</span>
              </span>
            </label>
            <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input v-model="form.notify_admin_alerts" type="checkbox" class="mt-1" />
              <span>
                {{ t('admin.telegramBot.notifications.adminAlerts') }}
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.notifications.adminAlertsHint') }}</span>
              </span>
            </label>
            <div class="flex flex-wrap items-center gap-3">
              <label class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input v-model="form.daily_report_enabled" type="checkbox" />
                <span>{{ t('admin.telegramBot.notifications.dailyReport') }}</span>
              </label>
              <label class="inline-flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
                <span>{{ t('admin.telegramBot.notifications.reportTime') }}</span>
                <input v-model="form.daily_report_time" type="time" class="input w-32" :disabled="!form.daily_report_enabled" />
              </label>
              <span class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.telegramBot.notifications.reportTimeHint', { timezone: settings.timezone }) }}
              </span>
            </div>
          </div>

          <div class="mt-6 flex flex-wrap gap-2">
            <button type="button" class="btn btn-primary btn-sm" :disabled="saving" @click="save">
              {{ saving ? t('common.loading') : t('admin.telegramBot.save') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="testing || !botActive" @click="sendTest">
              {{ testing ? t('common.loading') : t('admin.telegramBot.test') }}
            </button>
          </div>
        </div>

        <!-- Short notice -->
        <div class="card p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.telegramBot.notice.title') }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.notice.description') }}</p>
          <textarea
            v-model="noticeText"
            rows="3"
            class="input mt-4 w-full"
            :maxlength="settings.broadcast_max_length"
            :placeholder="t('admin.telegramBot.notice.placeholder')"
            :disabled="!botActive"
          ></textarea>
          <div class="mt-2 flex flex-wrap items-center justify-between gap-2">
            <span class="text-xs" :class="noticeLength > settings.broadcast_max_length ? 'text-red-600' : 'text-gray-500 dark:text-gray-400'">
              {{ noticeLength }} / {{ settings.broadcast_max_length }}
            </span>
            <span v-if="!botActive" class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.telegramBot.notice.needsBot') }}</span>
            <button type="button" class="btn btn-primary btn-sm" :disabled="!canSendNotice" @click="showNoticeDialog = true">
              {{ t('admin.telegramBot.notice.send') }}
            </button>
          </div>
        </div>
      </template>
    </div>

    <ConfirmDialog
      :show="showNoticeDialog"
      :title="t('admin.telegramBot.notice.confirmTitle')"
      :message="t('admin.telegramBot.notice.confirmMessage', { count: settings?.linked_users ?? 0 })"
      :confirm-text="t('admin.telegramBot.notice.send')"
      @confirm="sendNotice"
      @cancel="showNoticeDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { telegramAdminAPI, type TelegramBotSettings, type TelegramBotSettingsInput } from '@/api/admin/telegram'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const sendingNotice = ref(false)
const showNoticeDialog = ref(false)
const settings = ref<TelegramBotSettings | null>(null)
const noticeText = ref('')
const form = reactive<TelegramBotSettingsInput>({
  enabled: false,
  bot_token: '',
  proxy_url: '',
  notify_balance_low: true,
  notify_admin_alerts: true,
  daily_report_enabled: false,
  daily_report_time: '23:55',
  news_channel_url: '',
  group_invite_url: ''
})

// Count code points, matching the server's rune limit.
const noticeLength = computed(() => Array.from(noticeText.value.trim()).length)
const botActive = computed(() => Boolean(settings.value?.enabled && settings.value.token_configured))
const canSendNotice = computed(
  () => botActive.value && !sendingNotice.value && noticeLength.value > 0 && noticeLength.value <= (settings.value?.broadcast_max_length ?? 0)
)

const statusText = computed(() => {
  const s = settings.value
  if (!s || !s.enabled || !s.token_configured) return t('admin.telegramBot.setup.off')
  if (!s.last_poll_at) return t('admin.telegramBot.setup.waiting')
  if (s.last_error) return t('admin.telegramBot.setup.error')
  return `${t('admin.telegramBot.setup.receiving')} · ${t('admin.telegramBot.setup.lastPoll', { time: new Date(s.last_poll_at).toLocaleTimeString() })}`
})
const statusClass = computed(() =>
  settings.value?.last_error ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-white'
)

function errorMessage(error: unknown): string {
  return (error as { message?: string })?.message || t('errors.networkError')
}

function applySettings(next: TelegramBotSettings): void {
  settings.value = next
  form.enabled = next.enabled
  form.bot_token = ''
  form.proxy_url = next.proxy_url
  form.notify_balance_low = next.notify_balance_low
  form.notify_admin_alerts = next.notify_admin_alerts
  form.daily_report_enabled = next.daily_report_enabled
  form.daily_report_time = next.daily_report_time
  form.news_channel_url = next.news_channel_url
  form.group_invite_url = next.group_invite_url
}

async function load(): Promise<void> {
  loading.value = true
  try {
    applySettings(await telegramAdminAPI.getSettings())
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    loading.value = false
  }
}

async function save(): Promise<void> {
  saving.value = true
  try {
    applySettings(await telegramAdminAPI.updateSettings({ ...form }))
    appStore.showSuccess(t('admin.telegramBot.saved'))
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function sendTest(): Promise<void> {
  testing.value = true
  try {
    const { sent } = await telegramAdminAPI.sendTest()
    if (sent > 0) {
      appStore.showSuccess(t('admin.telegramBot.testSent', { count: sent }))
    } else {
      appStore.showWarning(t('admin.telegramBot.testNoAdmins'))
    }
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    testing.value = false
  }
}

async function sendNotice(): Promise<void> {
  showNoticeDialog.value = false
  sendingNotice.value = true
  try {
    const { recipients } = await telegramAdminAPI.broadcast(noticeText.value.trim())
    appStore.showSuccess(t('admin.telegramBot.notice.queued', { count: recipients }))
    noticeText.value = ''
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    sendingNotice.value = false
  }
}

onMounted(load)
</script>
