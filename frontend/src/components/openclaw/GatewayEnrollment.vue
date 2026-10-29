<template>
  <section class="gateway-enrollment" aria-labelledby="gateway-enrollment-title">
    <div class="gateway-heading"><div><h2 id="gateway-enrollment-title">{{ t('connections.gatewayEnrollment.title') }}</h2><p>{{ t('connections.gatewayEnrollment.description') }}</p></div></div>
    <div class="enrollment-actions">
      <button class="btn btn-secondary btn-sm" type="button" :disabled="busy || active" @click="create('ios')">{{ t('connections.gatewayEnrollment.iphone') }}</button>
      <button class="btn btn-secondary btn-sm" type="button" :disabled="busy || active" @click="create('macos')">{{ t('connections.gatewayEnrollment.macos') }}</button>
    </div>
    <p v-if="error" class="ag-error" role="alert">{{ error }}</p>
    <div v-if="enrollment" class="enrollment-code">
      <p><strong>{{ t(`connections.gatewayEnrollment.${enrollment.client === 'ios' ? 'scanTitle' : 'macSetupTitle'}`, { client: 'iPhone' }) }}</strong></p>
      <p class="enrollment-endpoint"><strong>{{ t('connections.gatewayEnrollment.endpoint') }}</strong> <code>{{ enrollment.gateway_url }}</code></p>
      <p class="enrollment-warning">{{ t('connections.gatewayEnrollment.nativeOnlyHint') }}</p>
      <canvas v-show="enrollment.state === 'created' && remainingSeconds > 0" ref="canvas" aria-label="OpenClaw device pairing QR code"></canvas>
      <p class="enrollment-expiry">{{ t('connections.gatewayEnrollment.expires', { time: new Date(enrollment.expires_at).toLocaleTimeString() }) }}</p>
      <p role="status">{{ t(`connections.gatewayEnrollment.state.${enrollment.state}`) }}</p>
      <p v-if="enrollment.delivery_uncertain" role="alert">{{ t('connections.gatewayEnrollment.deliveryUncertain') }}</p>
      <p v-if="remainingSeconds > 0">{{ t('connections.gatewayEnrollment.countdown', { seconds: remainingSeconds }) }}</p>
      <details v-if="enrollment.state === 'created' && remainingSeconds > 0"><summary>{{ t('connections.gatewayEnrollment.manual') }}</summary><code>{{ enrollment.setup_code }}</code></details>
      <p v-if="enrollment.state === 'created'">{{ t('connections.gatewayEnrollment.approvalHint') }}</p>
      <div v-if="enrollment.device" class="enrollment-review">
        <p><strong>{{ t('connections.gatewayEnrollment.fingerprint') }}</strong></p>
        <code>{{ enrollment.device.device_id }}</code>
        <p v-if="enrollment.device.display_name">{{ t('connections.gatewayDevices.claimedName') }}: {{ enrollment.device.display_name }}</p>
        <p v-if="enrollment.device.platform">{{ t('connections.gatewayDevices.claimedPlatform') }}: {{ enrollment.device.platform }}</p>
        <p>{{ t('connections.gatewayDevices.requested') }}: {{ [...enrollment.device.roles, ...enrollment.device.scopes].join(', ') }}</p>
        <small>{{ t('connections.gatewayDevices.requestId') }}: {{ enrollment.device.request_id }}</small>
        <p v-if="enrollment.client === 'ios'">{{ t('connections.gatewayEnrollment.capabilityHint') }}</p>
        <div v-if="enrollment.state === 'awaiting_approval' && remainingSeconds > 0" class="enrollment-actions">
          <button type="button" class="btn btn-primary btn-sm" :disabled="busy" @click="decide('approve')">{{ t('connections.gatewayDevices.approve') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="decide('reject')">{{ t('connections.gatewayDevices.reject') }}</button>
        </div>
      </div>
      <p v-if="enrollment.client === 'macos' && enrollment.state === 'connected'">{{ t('connections.gatewayEnrollment.macReconnectHint') }}</p>
      <button v-if="['created', 'awaiting_approval'].includes(enrollment.state)" type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="cancel">{{ t('connections.gatewayEnrollment.cancel') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import { cancelOpenClawGatewayEnrollment, createOpenClawGatewayEnrollment, getOpenClawGatewayEnrollment, updateOpenClawGatewayDevice, type OpenClawGatewayEnrollment } from '@/api/openclaw'

const props = defineProps<{ instanceId: number }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const busy = ref(false); const error = ref(''); const enrollment = ref<OpenClawGatewayEnrollment | null>(null); const canvas = ref<HTMLCanvasElement | null>(null)
let pollTimer: ReturnType<typeof setInterval> | undefined
let polling = false
let generation = 0
const now = ref(Date.now())
const remainingSeconds = computed(() => enrollment.value ? Math.max(0, Math.ceil((Date.parse(enrollment.value.expires_at) - now.value) / 1000)) : 0)
const active = computed(() => Boolean(enrollment.value && !enrollment.value.delivery_uncertain && ['created', 'awaiting_approval', 'approved'].includes(enrollment.value.state) && remainingSeconds.value > 0))
const clockTimer = setInterval(() => { now.value = Date.now() }, 1000)
function stopPolling() { generation++; if (pollTimer) { clearInterval(pollTimer); pollTimer = undefined } }
function startPolling() {
  stopPolling()
  const currentGeneration = generation
  pollTimer = setInterval(async () => {
    if (!enrollment.value || ['connected', 'expired', 'rejected', 'cancelled'].includes(enrollment.value.state)) { stopPolling(); return }
    if (polling) return
    polling = true
    const current = enrollment.value
    try {
      const status = await getOpenClawGatewayEnrollment(props.instanceId, current.id)
      if (generation === currentGeneration && enrollment.value?.id === current.id) {
        enrollment.value = { ...current, ...status }
        error.value = ''
        if (status.state !== current.state) emit('changed')
      }
    } catch { error.value = t('connections.gatewayEnrollment.statusFailed') }
    finally { polling = false }
  }, 2000)
}
async function create(client: 'ios' | 'macos') {
  if (busy.value || active.value) return
  busy.value = true; error.value = ''; enrollment.value = null; stopPolling()
  try { enrollment.value = await createOpenClawGatewayEnrollment(props.instanceId, client); await nextTick(); if (canvas.value && enrollment.value) await QRCode.toCanvas(canvas.value, enrollment.value.setup_code, { width: 240, margin: 2 }); startPolling() }
  catch { error.value = t('connections.gatewayEnrollment.failed') }
  finally { busy.value = false }
}
async function decide(action: 'approve' | 'reject') {
  const current = enrollment.value
  if (busy.value || !current?.device || current.state !== 'awaiting_approval' || remainingSeconds.value <= 0) return
  const device = current.device
  if (action === 'approve' && !window.confirm(t('connections.gatewayDevices.approveConfirm', { device: current.client === 'ios' ? 'iPhone' : 'macOS', id: device.device_id, access: [...device.roles, ...device.scopes].join(', ') }))) return
  busy.value = true; error.value = ''; stopPolling()
  try {
    await updateOpenClawGatewayDevice(props.instanceId, action, { deviceId: device.device_id, requestId: device.request_id })
    const status = await getOpenClawGatewayEnrollment(props.instanceId, current.id)
    enrollment.value = { ...current, ...status }
    emit('changed')
  } catch { error.value = t('connections.gatewayDevices.failed') }
  finally { busy.value = false; startPolling() }
}
async function cancel() {
  if (!enrollment.value || busy.value) return
  busy.value = true
  stopPolling()
  try { await cancelOpenClawGatewayEnrollment(props.instanceId, enrollment.value.id); enrollment.value = { ...enrollment.value, state: 'cancelled' }; emit('changed') }
  catch { error.value = t('connections.gatewayEnrollment.failed') }
  finally { busy.value = false; if (enrollment.value && ['created', 'awaiting_approval'].includes(enrollment.value.state)) startPolling() }
}
onBeforeUnmount(() => { stopPolling(); clearInterval(clockTimer) })
</script>

<style scoped>
.gateway-enrollment { padding: 1.5rem; border: 1px solid var(--ag-border, #333); border-radius: 1rem; margin-block: 1.5rem; }
.gateway-heading h2 { font-size: 1.2rem; font-weight: 600; }
.gateway-heading p { margin-block: .5rem 1rem; }
.enrollment-actions { display: flex; flex-wrap: wrap; gap: .75rem; }
.enrollment-code { display: grid; gap: .75rem; justify-items: start; margin-top: 1rem; }
.enrollment-endpoint, .enrollment-warning { max-width: 42rem; overflow-wrap: anywhere; }
.enrollment-warning { color: var(--ag-muted, #aab); }
.enrollment-code canvas { background: white; padding: .5rem; border-radius: .5rem; }
.enrollment-code code { display: block; max-width: 100%; overflow-wrap: anywhere; font-size: .7rem; }
.enrollment-review { display: grid; gap: .75rem; max-width: 100%; padding-block: .75rem; border-block: 1px solid var(--ag-border, #333); }
.enrollment-review code, .enrollment-review small { overflow-wrap: anywhere; }
</style>
