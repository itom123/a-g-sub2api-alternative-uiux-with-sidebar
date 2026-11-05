<template>
  <div class="relative" ref="dropdownRef">
    <button
      @click="showDropdown = !showDropdown"
      class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 text-xs font-medium text-gray-700 shadow-sm transition-colors hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700"
      :title="t('common.autoRefresh.title')"
    >
      <Icon name="clock" size="compact" />
      <span>
        {{ enabled
          ? t('common.autoRefresh.countdown', { seconds: countdown })
          : t('common.autoRefresh.title')
        }}
      </span>
    </button>

    <div
      v-if="showDropdown"
      class="absolute right-0 z-20 mt-1 w-44 rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-800"
    >
      <div class="p-1.5">
        <button
          @click="$emit('update:enabled', !enabled)"
          class="flex w-full items-center justify-between rounded-md px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
        >
          <span>{{ t('common.autoRefresh.enable') }}</span>
          <Icon name="check" size="sm" v-if="enabled" class="text-primary-500" />
        </button>
        <div class="my-1 border-t border-gray-100 dark:border-dark-700"></div>
        <button
          v-for="sec in intervals"
          :key="sec"
          @click="$emit('update:interval', sec)"
          class="flex w-full items-center justify-between rounded-md px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
        >
          <span>{{ t('common.autoRefresh.seconds', { n: sec }) }}</span>
          <Icon name="check" size="sm" v-if="intervalSeconds === sec" class="text-primary-500" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps<{
  enabled: boolean
  intervalSeconds: number
  countdown: number
  intervals: readonly number[]
}>()

defineEmits<{
  (e: 'update:enabled', value: boolean): void
  (e: 'update:interval', value: number): void
}>()

const { t } = useI18n()
const showDropdown = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    showDropdown.value = false
  }
}

onMounted(() => document.addEventListener('click', handleClickOutside))
onBeforeUnmount(() => document.removeEventListener('click', handleClickOutside))
</script>
