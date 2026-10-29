<template>
  <div class="ag-page-loading" aria-live="polite">
    <LoadingSpinner />
    <div v-if="$slots.default" class="ag-page-loading-caption"><slot /></div>
  </div>
</template>

<script setup lang="ts">
import LoadingSpinner from './LoadingSpinner.vue'
</script>

<style scoped>
.ag-page-loading {
  position: fixed;
  inset: 0;
  margin: 0 !important;
  z-index: 20;
  display: grid;
  place-items: center;
  background: var(--ag-paper);
}

.ag-page-loading-caption {
  position: absolute;
  top: calc(50% + 32px);
  left: 0;
  right: 0;
  padding: 0 24px;
  text-align: center;
}
</style>
