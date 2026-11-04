<template>
  <div class="card flex items-start justify-between gap-4 p-4" data-testid="balance-overflow-card">
    <div class="min-w-0">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('userSubscriptions.balanceOverflow.title') }}
      </h3>
      <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
        {{ t('userSubscriptions.balanceOverflow.description') }}
      </p>
      <p
        v-if="!adminAllowed"
        class="mt-1 text-xs text-amber-700 dark:text-amber-300"
        data-testid="balance-overflow-admin-disabled"
      >
        {{ t('userSubscriptions.balanceOverflow.adminDisabled') }}
      </p>
    </div>
    <Toggle
      :model-value="adminAllowed && enabled"
      :disabled="!adminAllowed || saving"
      data-testid="balance-overflow-toggle"
      @update:model-value="handleToggle"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { userAPI } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const saving = ref(false)

// 两个开关都开才生效：管理员许可（只读）+ 用户自己的偏好。缺省按开启处理（与后端默认一致）。
const adminAllowed = computed(() => authStore.user?.balance_overflow_admin_allowed ?? true)
const enabled = computed(() => authStore.user?.balance_overflow_enabled ?? true)

const handleToggle = async (value: boolean) => {
  if (!adminAllowed.value || saving.value) return
  saving.value = true
  try {
    const updated = await userAPI.updateProfile({ balance_overflow_enabled: value })
    authStore.user = updated
    appStore.showSuccess(t('userSubscriptions.balanceOverflow.saved'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    saving.value = false
  }
}
</script>
