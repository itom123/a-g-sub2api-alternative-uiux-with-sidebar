<script setup lang="ts">
/**
 * 导航进度条组件
 * 在屏幕中央显示导航加载状态
 */
import { computed } from 'vue'
import { useNavigationLoadingState } from '@/composables/useNavigationLoading'
import PageLoading from './PageLoading.vue'

const { isLoading } = useNavigationLoadingState()

// 进度条可见性
const isVisible = computed(() => isLoading.value)
</script>

<template>
  <Transition name="progress-fade">
    <PageLoading v-show="isVisible" class="navigation-progress" />
  </Transition>
</template>

<style scoped>
.navigation-progress {
  z-index: 9999;
  pointer-events: none;
}

/* 淡入淡出过渡 */
.progress-fade-enter-active {
  transition: opacity 0.15s ease-out;
}

.progress-fade-leave-active {
  transition: opacity 0.3s ease-out;
}

.progress-fade-enter-from,
.progress-fade-leave-to {
  opacity: 0;
}

</style>
