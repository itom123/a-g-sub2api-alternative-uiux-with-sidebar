<template>
  <div class="connect-landing">
    <main class="connect-card">
      <p class="connect-kicker">AG_&gt; · 链接</p>
      <h1>{{ t('connections.landingTitle') }}</h1>
      <p class="connect-lede">{{ t('connections.landingDescription') }}</p>
      <div class="connect-actions">
        <RouterLink class="btn btn-primary" to="/login?redirect=/connections">{{ t('connections.signIn') }}</RouterLink>
        <RouterLink class="btn btn-secondary" to="/register?redirect=/connections">{{ t('connections.createAccount') }}</RouterLink>
      </div>
      <p class="connect-note">{{ t('connections.landingNote') }}</p>
    </main>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<style scoped>
.connect-landing { min-height: 100vh; display: grid; place-items: center; padding: 32px; background: var(--ag-paper, #f4f1e8); color: var(--ag-ink, #1f2522); }
.connect-card { width: min(640px, 100%); min-width: 0; border: 1px solid currentColor; padding: clamp(20px, 6vw, 64px); background: var(--ag-surface); }
.connect-kicker { margin: 0 0 28px; font-size: 13px; letter-spacing: .18em; text-transform: uppercase; }
h1 { margin: 0; font-size: clamp(36px, 7vw, 72px); line-height: .98; }
.connect-lede { max-width: 48ch; margin: 24px 0 0; font-size: 18px; line-height: 1.65; }
.connect-actions { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 32px; }
.connect-note { margin: 28px 0 0; font-size: 13px; line-height: 1.6; opacity: .75; }
</style>
