<template>
  <AppLayout>
    <div class="mx-auto max-w-[950px] space-y-6">
      <ProfileTelegramCard />
      <p v-if="telegramStatusLoaded && !telegramAvailable" class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('telegramBot.unavailable') }}
      </p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import ProfileTelegramCard from '@/components/user/profile/ProfileTelegramCard.vue'
import { useTelegramAvailability } from '@/composables/useTelegramAvailability'

const { t } = useI18n()
const { telegramAvailable, telegramStatusLoaded } = useTelegramAvailability()
</script>
