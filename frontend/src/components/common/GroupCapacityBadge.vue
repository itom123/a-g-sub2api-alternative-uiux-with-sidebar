<template>
  <div class="flex flex-col gap-1">
    <!-- 并发槽位 -->
    <div class="flex items-center gap-1">
      <span
        :class="[
          'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium',
          capacityClass(concurrencyUsed, concurrencyMax)
        ]"
      >
        <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="square" stroke-linejoin="miter" d="M3 3H10V10H3ZM14 3H21V10H14ZM3 14H10V21H3ZM14 14H21V21H14Z" />
        </svg>
        <span class="font-mono">{{ concurrencyUsed }}</span>
        <span class="text-gray-400 dark:text-gray-500">/</span>
        <span class="font-mono">{{ concurrencyMax }}</span>
      </span>
    </div>

    <!-- 会话数 -->
    <div v-if="sessionsMax > 0" class="flex items-center gap-1">
      <span
        :class="[
          'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium',
          capacityClass(sessionsUsed, sessionsMax)
        ]"
      >
        <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="square" stroke-linejoin="miter" fill="none" stroke="currentColor" stroke-width="1.5" d="M9 2H15V8H9ZM3 6H6V11H3ZM18 6H21V11H18ZM7 22V14L10 11H14L17 14V22ZM7 14H2V22H22V14H17" />
        </svg>
        <span class="font-mono">{{ sessionsUsed }}</span>
        <span class="text-gray-400 dark:text-gray-500">/</span>
        <span class="font-mono">{{ sessionsMax }}</span>
      </span>
    </div>

    <!-- RPM -->
    <div v-if="rpmMax > 0" class="flex items-center gap-1">
      <span
        :class="[
          'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium',
          capacityClass(rpmUsed, rpmMax)
        ]"
      >
        <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
          <path stroke-linecap="square" stroke-linejoin="miter" fill="none" stroke="currentColor" stroke-width="1.5" d="M3 3H21V21H3ZM12 6V12H17" />
        </svg>
        <span class="font-mono">{{ rpmUsed }}</span>
        <span class="text-gray-400 dark:text-gray-500">/</span>
        <span class="font-mono">{{ rpmMax }}</span>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
interface Props {
  concurrencyUsed: number
  concurrencyMax: number
  sessionsUsed: number
  sessionsMax: number
  rpmUsed: number
  rpmMax: number
}

withDefaults(defineProps<Props>(), {
  concurrencyUsed: 0,
  concurrencyMax: 0,
  sessionsUsed: 0,
  sessionsMax: 0,
  rpmUsed: 0,
  rpmMax: 0
})

function capacityClass(used: number, max: number): string {
  if (max > 0 && used >= max) {
    return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  }
  if (used > 0) {
    return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  }
  return 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
}
</script>
