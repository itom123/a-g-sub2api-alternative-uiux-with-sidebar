<template>
  <div ref="grid" class="ag-stat-grid grid grid-cols-2 gap-4 lg:grid-cols-4" :style="columnStyle"><slot /></div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useElementSize, useMediaQuery } from '@vueuse/core'

const grid = ref<HTMLElement | null>(null)
const { width } = useElementSize(grid)
const desktop = useMediaQuery('(min-width: 1024px)')
const columnStyle = computed(() => {
  if (!width.value) return undefined
  const columns = desktop.value ? 4 : 2
  // Adjacent one-pixel borders overlap; keep the tracks on whole pixels too.
  const track = Math.max(0, Math.floor(width.value / columns))
  return { gridTemplateColumns: `repeat(${columns - 1}, minmax(0, ${track}px)) minmax(0, 1fr)` }
})
</script>
