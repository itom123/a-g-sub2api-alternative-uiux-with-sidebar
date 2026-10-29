import { computed, ref, type ComputedRef } from 'vue'
import { telegramAPI } from '@/api/telegram'
import { useAuthStore } from '@/stores/auth'

// Whether the site's Telegram bot is on. Shared app-wide so the sidebar and
// pages ask the server once per page load; a failed check is retried later.
const available = ref(false)
const loaded = ref(false)
let pending: Promise<void> | null = null

function load(): Promise<void> {
  if (loaded.value || !useAuthStore().isAuthenticated) return Promise.resolve()
  pending ??= telegramAPI
    .getStatus()
    .then((status) => {
      available.value = status.available === true
      loaded.value = true
    })
    .catch(() => {
      available.value = false
    })
    .finally(() => {
      pending = null
    })
  return pending
}

export function useTelegramAvailability(): {
  telegramAvailable: ComputedRef<boolean>
  telegramStatusLoaded: ComputedRef<boolean>
} {
  void load()
  return {
    telegramAvailable: computed(() => available.value),
    telegramStatusLoaded: computed(() => loaded.value)
  }
}
