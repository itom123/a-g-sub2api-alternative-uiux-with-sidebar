<template>
  <AppLayout>
  <div class="ag-page career-concierge-page">
    <header class="page-header">
      <div>
        <p class="section-kicker">Career Concierge</p>
        <h1>Personalised job advice</h1>
        <p class="page-description">Your shared OpenClaw career assistant can send tailored job briefings to your Telegram chat.</p>
      </div>
    </header>

    <div class="career-panels">
    <section class="panel profile-panel">
      <h2>Your profile</h2>
      <div class="form-grid">
        <label>Display name<input v-model="form.display_name" maxlength="120" /></label>
        <label>Minimum salary<input v-model.number="form.salary_min" type="number" min="0" step="1000" /></label>
        <label>Roles and keywords<textarea v-model="form.roles" rows="3" placeholder="e.g. producer, arts administrator, creative technology" /></label>
        <label>Locations<textarea v-model="form.locations" rows="3" placeholder="e.g. London, UK remote, Europe" /></label>
        <label class="full-width">Timezone<input v-model="form.timezone" placeholder="Europe/London" /></label>
        <label class="full-width">Additional context<textarea v-model="form.notes" rows="3" placeholder="Experience, exclusions, organisations, or advice preferences" /></label>
      </div>
      <label class="checkbox"><input v-model="form.remote_ok" type="checkbox" /> Include remote roles</label>
      <button class="btn btn-primary" :disabled="saving" @click="saveProfile">{{ saving ? 'Saving…' : 'Save profile' }}</button>
    </section>

    <div class="career-secondary">
    <section class="panel">
      <h2>Telegram delivery</h2>
      <p v-if="profile?.telegram_paired" class="success">Telegram is paired. Briefings will be sent to your verified chat.</p>
      <p v-else>Generate a short-lived code, then send <code>/career_pair CODE</code> to <a href="https://t.me/SMSREPORTERBOT" target="_blank" rel="noopener noreferrer">the shared Telegram bot</a>.</p>
      <div class="pair-row">
        <button class="btn btn-secondary" :disabled="pairing" @click="generatePairing">{{ pairing ? 'Generating…' : 'Generate pairing code' }}</button>
        <code v-if="pairingCode" class="pair-code">{{ pairingCode }}</code>
      </div>
      <button v-if="pairingCode" class="btn btn-secondary" @click="refreshPairing">Refresh pairing status</button>
      <p v-if="pairingExpiry" class="muted">Expires {{ pairingExpiry }}</p>
      <button v-if="profile?.telegram_paired" class="btn btn-danger btn-sm" @click="unpair">Disconnect Telegram</button>
    </section>

    <section class="panel">
      <div class="section-heading"><h2>Briefing schedule</h2><span class="muted">Five-field cron</span></div>
      <div class="schedule-row">
        <input v-model="cronExpression" placeholder="0 9 * * 1-5" aria-label="Cron expression" />
        <input v-model="form.timezone" placeholder="Europe/London" aria-label="Schedule timezone" />
        <button class="btn btn-primary" :disabled="!profile?.telegram_paired || creatingSchedule" @click="addSchedule">{{ creatingSchedule ? 'Adding…' : 'Add schedule' }}</button>
      </div>
      <p v-if="!profile?.telegram_paired" class="muted">Pair Telegram before adding a schedule.</p>
      <div v-if="schedules.length" class="schedule-list">
        <div v-for="schedule in schedules" :key="schedule.id" class="schedule-item">
          <code>{{ schedule.cron_expression }}</code><span>{{ schedule.timezone }}</span>
          <span v-if="schedule.last_error" class="error">{{ schedule.last_error }}</span>
          <span :class="schedule.enabled ? 'success' : 'muted'">{{ schedule.enabled ? 'Enabled' : 'Disabled' }}</span>
          <button class="btn btn-danger btn-sm" @click="removeSchedule(schedule.id)">Remove</button>
        </div>
      </div>
      <p v-else class="muted">No briefing schedules yet.</p>
    </section>

    </div>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
  </div>
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
import { onMounted, reactive, ref } from 'vue'
import {
  createCareerPairing,
  createCareerSchedule,
  deleteCareerSchedule,
  getCareerProfile,
  listCareerSchedules,
  unpairCareerTelegram,
  updateCareerProfile,
  type CareerProfile,
  type CareerSchedule,
} from '@/api/openclaw'

const profile = ref<CareerProfile | null>(null)
const schedules = ref<CareerSchedule[]>([])
const form = reactive({ display_name: '', roles: '', locations: '', remote_ok: true, salary_min: 0, notes: '', timezone: 'Europe/London' })
const cronExpression = ref('0 9 * * 1-5')
const pairingCode = ref('')
const pairingExpiry = ref('')
const saving = ref(false)
const pairing = ref(false)
const creatingSchedule = ref(false)
const error = ref('')

async function load() {
  try {
    const [nextProfile, nextSchedules] = await Promise.all([getCareerProfile(), listCareerSchedules()])
    profile.value = nextProfile
    schedules.value = nextSchedules
    Object.assign(form, { display_name: nextProfile.display_name, roles: nextProfile.roles, locations: nextProfile.locations, remote_ok: nextProfile.remote_ok, salary_min: nextProfile.salary_min, notes: nextProfile.notes, timezone: nextProfile.timezone })
  } catch (err) { error.value = err instanceof Error ? err.message : 'Unable to load career profile' }
}

async function refreshPairing() {
  try {
    profile.value = await getCareerProfile()
    if (profile.value.telegram_paired) { pairingCode.value = ''; pairingExpiry.value = '' }
  } catch (err) { error.value = err instanceof Error ? err.message : 'Unable to refresh pairing' }
}

async function saveProfile() {
  saving.value = true; error.value = ''
  try { profile.value = await updateCareerProfile({ ...form }) } catch (err) { error.value = err instanceof Error ? err.message : 'Unable to save profile' } finally { saving.value = false }
}

async function unpair() {
  try { await unpairCareerTelegram(); pairingCode.value = ''; pairingExpiry.value = ''; await load() } catch (err) { error.value = err instanceof Error ? err.message : 'Unable to disconnect Telegram' }
}

async function generatePairing() {
  pairing.value = true; error.value = ''
  try { const result = await createCareerPairing(); pairingCode.value = result.code; pairingExpiry.value = new Date(result.expires_at).toLocaleString() } catch (err) { error.value = err instanceof Error ? err.message : 'Unable to create pairing code' } finally { pairing.value = false }
}

async function addSchedule() {
  creatingSchedule.value = true; error.value = ''
  try { schedules.value.unshift(await createCareerSchedule({ cron_expression: cronExpression.value, timezone: form.timezone, enabled: true })) } catch (err) { error.value = err instanceof Error ? err.message : 'Unable to add schedule' } finally { creatingSchedule.value = false }
}

async function removeSchedule(id: number) {
  try { await deleteCareerSchedule(id); schedules.value = schedules.value.filter(item => item.id !== id) } catch (err) { error.value = err instanceof Error ? err.message : 'Unable to remove schedule' }
}

onMounted(load)
</script>

<style scoped>
.career-concierge-page { width: 100%; max-width: none; min-width: 0; padding: 4px 0 48px; container: career / inline-size; }
.career-panels, .career-secondary { display: grid; gap: 24px; align-items: start; }
.career-panels > *, .career-secondary > * { min-width: 0; }
.page-header { margin-bottom: 1.5rem; }
.page-description, .muted { color: var(--ag-ink); }
.panel { margin: 0; padding: 1.25rem; border: 1px solid var(--ag-rule); background: var(--ag-surface); }
.form-grid { align-items: start; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1rem; }
.full-width { grid-column: 1 / -1; }
label { display: grid; min-width: 0; gap: .35rem; font-weight: 600; }
input, textarea { width: 100%; min-width: 0; border: 1px solid var(--ag-rule); padding: .65rem; font: inherit; background: var(--ag-paper); color: var(--ag-ink); }
.checkbox { display: flex; grid-template-columns: auto 1fr; margin: 1rem 0; align-items: center; }
.checkbox input { width: auto; }
.btn { border: 0; border-radius: 8px; padding: .6rem .9rem; cursor: pointer; }
.btn-sm { padding: .35rem .6rem; }
.pair-row, .schedule-row, .section-heading, .schedule-item { display: flex; align-items: center; gap: .75rem; }
.pair-row, .section-heading, .schedule-item { flex-wrap: wrap; }
.pair-code { font-size: 1.3rem; letter-spacing: .15em; }
.schedule-row { flex-wrap: wrap; }
.schedule-row input { flex: 1 1 160px; min-width: 0; }
.schedule-row .btn { flex: 1 0 auto; }
.panel p, .schedule-item > *, .pair-code { overflow-wrap: anywhere; min-width: 0; }
textarea { resize: vertical; }
.schedule-list { display: grid; gap: .5rem; margin-top: 1rem; }
.schedule-item { justify-content: space-between; padding: .65rem; border-top: 1px solid var(--ag-rule); }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
@container career (min-width: 1100px) {
  .career-panels { grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr); }
}
@container career (max-width: 639px) {
  .form-grid { grid-template-columns: 1fr; }
  .panel { padding: 16px; }
  .schedule-row input, .schedule-row .btn { flex-basis: 100%; }
  .pair-row .btn { width: 100%; }
}
</style>
