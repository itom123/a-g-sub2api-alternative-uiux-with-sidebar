<template>
  <button
    class="btn btn-secondary ag-theme-toggle"
    type="button"
    :aria-pressed="isDark"
    :aria-label="targetMode"
    :title="targetMode"
    @click="toggleTheme()"
  >
    <Icon :name="isDark ? 'sun' : 'moon'" size="md" aria-hidden="true" />
    <span class="sr-only">{{ targetMode }}</span>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useColorMode } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const mode = useColorMode({ storageKey: 'theme', modes: { dark: 'dark', light: 'light' } })
const isDark = computed(() => mode.value === 'dark')
function toggleTheme() {
  mode.value = isDark.value ? 'light' : 'dark'
}
const targetMode = computed(() => isDark.value ? t('nav.lightMode') : t('nav.darkMode'))
</script>
