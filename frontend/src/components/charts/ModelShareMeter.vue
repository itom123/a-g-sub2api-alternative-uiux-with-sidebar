<template>
  <div ref="track" class="ag-share-meter" role="img" :aria-label="label">
    <span
      class="ag-share-fill"
      :data-positive="share > 0"
      :data-pattern="patternName"
      :style="{ width: `${share}%` }"
    >
      <svg width="100%" height="100%" aria-hidden="true">
        <defs>
          <pattern :id="patternId" patternUnits="userSpaceOnUse" :y="patternOffset" width="8" height="8">
            <path v-if="patternIndex === 1" d="M-4 4L4-4M0 8L8 0M4 12L12 4" fill="none" stroke="currentColor" stroke-width="1" />
            <circle v-else-if="patternIndex === 2" cx="4" cy="4" r="1" fill="currentColor" />
            <path v-else-if="patternIndex === 3" d="M4 0V8M0 4H8" fill="none" stroke="currentColor" stroke-width="1" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" :fill="patternName === 'solid' ? 'currentColor' : `url(#${patternId})`" />
      </svg>
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { useElementSize } from '@vueuse/core'

const props = defineProps<{ share: number; index: number; label: string }>()
const track = ref<HTMLElement | null>(null)
const { width, height } = useElementSize(track)
const share = computed(() => Math.max(0, Math.min(100, props.share)))
const patternIndex = computed(() => props.index % 4)
const patternId = `share-${useId()}`
// Whole tiles rarely fill the track height; split the remainder between top and bottom.
const patternOffset = computed(() => height.value % 8 / 2)
// Hatching cannot be read below one pattern tile; retain the exact share label.
const patternName = computed(() => width.value * share.value / 100 < 12
  ? 'solid' : ['solid', 'diagonal', 'dots', 'grid'][patternIndex.value])
</script>
