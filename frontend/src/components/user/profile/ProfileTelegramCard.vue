<template>
  <div v-if="status?.available" class="card" data-testid="profile-telegram-card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-medium text-gray-900 dark:text-white">
        {{ t('telegramBot.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('telegramBot.description') }}
      </p>
    </div>

    <div class="px-6 py-6">
      <!-- Linked -->
      <div v-if="status.linked" class="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p class="font-medium text-gray-900 dark:text-white">{{ t('telegramBot.linked') }}</p>
          <p v-if="status.telegram_username" class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('telegramBot.linkedAs', { username: '@' + status.telegram_username }) }}
          </p>
          <p v-if="status.linked_at" class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('telegramBot.linkedAt', { date: formatDate(status.linked_at) }) }}
          </p>
        </div>
        <button type="button" class="btn btn-outline-danger" :disabled="busy" @click="showUnlinkDialog = true">
          {{ t('telegramBot.unlink') }}
        </button>
      </div>

      <!-- Not linked -->
      <div v-else class="space-y-4">
        <div>
          <p class="font-medium text-gray-700 dark:text-gray-300">{{ t('telegramBot.notLinked') }}</p>
          <p class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('telegramBot.notLinkedHint') }}
            <span class="font-mono">@{{ status.bot_username }}</span>
          </p>
        </div>

        <div v-if="status.bind_code" class="space-y-3 rounded-lg bg-gray-50 p-4 dark:bg-dark-800">
          <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('telegramBot.codeHint') }}</p>
          <div class="flex items-center gap-2">
            <code class="flex-1 truncate rounded bg-white px-3 py-2 font-mono text-sm dark:bg-dark-900" data-testid="telegram-bind-command">{{ bindCommand }}</code>
            <button type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(bindCommand, t('telegramBot.copied'))">
              {{ t('telegramBot.copy') }}
            </button>
          </div>
          <div class="flex flex-wrap gap-2">
            <a :href="openLink" target="_blank" rel="noopener noreferrer" class="btn btn-primary">
              {{ t('telegramBot.openTelegram') }}
            </a>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="checkLinked">
              {{ t('telegramBot.checkLinked') }}
            </button>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="showResetDialog = true">
              {{ t('telegramBot.resetCode') }}
            </button>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('telegramBot.codePrivate') }}</p>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :show="showUnlinkDialog"
      :title="t('telegramBot.unlinkTitle')"
      :message="t('telegramBot.unlinkConfirm')"
      :confirm-text="t('telegramBot.unlink')"
      danger
      @confirm="unlink"
      @cancel="showUnlinkDialog = false"
    />
    <ConfirmDialog
      :show="showResetDialog"
      :title="t('telegramBot.resetTitle')"
      :message="t('telegramBot.resetConfirm')"
      :confirm-text="t('telegramBot.resetCode')"
      @confirm="resetCode"
      @cancel="showResetDialog = false"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { telegramAPI, type TelegramLinkStatus } from '@/api/telegram'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const status = ref<TelegramLinkStatus | null>(null)
const busy = ref(false)
const showUnlinkDialog = ref(false)
const showResetDialog = ref(false)

const bindCommand = computed(() => (status.value?.bind_code ? `/bind ${status.value.bind_code}` : ''))
// One-tap link: Telegram sends "/start CODE" when the user presses Start.
const openLink = computed(() =>
  status.value?.bind_code ? `https://t.me/${status.value.bot_username}?start=${status.value.bind_code}` : ''
)

function errorMessage(error: unknown): string {
  return (error as { message?: string })?.message || t('errors.networkError')
}

async function loadStatus(): Promise<void> {
  try {
    status.value = await telegramAPI.getStatus()
  } catch {
    status.value = null // hide the card if the bot is unavailable
  }
}

async function resetCode(): Promise<void> {
  showResetDialog.value = false
  busy.value = true
  try {
    status.value = await telegramAPI.resetBindCode()
    appStore.showSuccess(t('telegramBot.codeReset'))
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    busy.value = false
  }
}

async function checkLinked(): Promise<void> {
  busy.value = true
  try {
    await loadStatus()
    if (status.value?.linked) {
      appStore.showSuccess(t('telegramBot.linkedSuccess'))
    } else {
      appStore.showInfo(t('telegramBot.stillPending'))
    }
  } finally {
    busy.value = false
  }
}

async function unlink(): Promise<void> {
  showUnlinkDialog.value = false
  busy.value = true
  try {
    await telegramAPI.unlink()
    appStore.showSuccess(t('telegramBot.unlinked'))
    await loadStatus()
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    busy.value = false
  }
}

function formatDate(value: string): string {
  return new Date(value).toLocaleDateString()
}

onMounted(loadStatus)
</script>
