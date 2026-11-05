<template>
  <svg
    :class="sizeClass"
    fill="none"
    viewBox="0 0 24 24"
    stroke="currentColor"
    :stroke-width="strokeWidth"
  >
    <path stroke-linecap="square" stroke-linejoin="miter" :d="iconPath" />
  </svg>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { iconPaths, type IconName } from './iconPaths'

const props = withDefaults(defineProps<{
  name: IconName
  size?: '2xs' | 'xs' | 'compact' | 'sm' | 'md' | 'lg' | 'xl' | '2xl' | '3xl'
  strokeWidth?: number
}>(), {
  size: 'md',
  strokeWidth: 1.5
})

const iconPath = computed(() => iconPaths[props.name])

const sizeClass = computed(() => ({
  '2xs': 'h-2.5 w-2.5',
  xs: 'h-3 w-3',
  compact: 'h-3.5 w-3.5',
  sm: 'h-4 w-4',
  md: 'h-5 w-5',
  lg: 'h-6 w-6',
  xl: 'h-8 w-8',
  '2xl': 'h-10 w-10',
  '3xl': 'h-12 w-12'
}[props.size]))
</script>
