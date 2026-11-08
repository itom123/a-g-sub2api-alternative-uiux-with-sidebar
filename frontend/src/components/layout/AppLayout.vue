<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="ag-app-main relative min-h-screen min-w-0 transition-[margin] duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-0' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="ag-workspace p-4 md:p-6" :style="{ '--ag-sidebar-width': sidebarCollapsed ? '0px' : '256px' }">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const appStore = useAppStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
</script>
