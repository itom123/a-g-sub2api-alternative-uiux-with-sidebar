<template>
  <section class="gateway-devices" aria-labelledby="gateway-devices-title">
    <div class="gateway-heading">
      <div><h2 id="gateway-devices-title">{{ t('connections.gatewayDevices.title') }}</h2><p>{{ t('connections.gatewayDevices.description') }}</p></div>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="refresh">{{ t('connections.gatewayDevices.refresh') }}</button>
    </div>
    <p v-if="error" role="alert" class="ag-error">{{ error }}</p>
    <p v-if="busy" role="status">{{ t('connections.gatewayDevices.loading') }}</p>
    <template v-if="inventory">
      <h3>{{ t('connections.gatewayDevices.pending') }}</h3>
      <p v-if="!inventory.pending.length">{{ t('connections.gatewayDevices.noPending') }}</p>
      <article v-for="device in inventory.pending" :key="device.requestId" class="gateway-device">
        <strong>{{ name(device) }}</strong>
        <p v-if="device.platform">{{ t('connections.gatewayDevices.claimedPlatform') }}: {{ device.platform }}</p>
        <small>{{ t('connections.gatewayDevices.identityHint') }}</small>
        <code>{{ device.deviceId }}</code>
        <p>{{ t('connections.gatewayDevices.requested') }}: {{ access(device) }}</p>
        <p v-if="approved(device)">{{ t('connections.gatewayDevices.currentAccess') }}: {{ approved(device) }}</p>
        <small>{{ t('connections.gatewayDevices.requestId') }}: {{ device.requestId }}</small>
        <div class="gateway-device-actions">
          <button type="button" class="btn btn-primary btn-sm" :disabled="busy" @click="change('approve', device)">{{ t('connections.gatewayDevices.approve') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="change('reject', device)">{{ t('connections.gatewayDevices.reject') }}</button>
        </div>
      </article>
      <h3>{{ t('connections.gatewayDevices.paired') }}</h3>
      <p v-if="!inventory.paired.length">{{ t('connections.gatewayDevices.noPaired') }}</p>
      <article v-for="device in inventory.paired" :key="device.deviceId" class="gateway-device">
        <strong>{{ name(device) }}</strong>
        <p v-if="device.platform">{{ t('connections.gatewayDevices.claimedPlatform') }}: {{ device.platform }}</p>
        <code>{{ device.deviceId }}</code>
        <p>{{ access(device) }}</p>
        <small v-if="device.lastSeenAtMs">{{ t('connections.gatewayDevices.lastSeen') }}: {{ new Date(device.lastSeenAtMs).toLocaleString() }}</small>
        <div class="gateway-device-actions"><button type="button" class="btn btn-danger btn-sm" :disabled="busy" @click="change('remove', device)">{{ t('connections.gatewayDevices.revoke') }}</button></div>
      </article>
    </template>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getOpenClawGatewayDevices, updateOpenClawGatewayDevice, type OpenClawGatewayDevice, type OpenClawGatewayInventory } from '@/api/openclaw'

const props = defineProps<{ instanceId: number }>()
const { t } = useI18n()
const inventory = ref<OpenClawGatewayInventory | null>(null)
const busy = ref(false)
const error = ref('')
const name = (d: OpenClawGatewayDevice) => d.operatorLabel || d.displayName || d.clientId || d.deviceId
const access = (d: OpenClawGatewayDevice) => [...(d.roles || (d.role ? [d.role] : [])), ...(d.scopes || [])].join(', ')
const approved = (d: OpenClawGatewayDevice) => {
  const paired = inventory.value?.paired.find(p => p.deviceId === d.deviceId)
  return paired ? access(paired) : ''
}

async function refresh() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  inventory.value = null
  try { inventory.value = await getOpenClawGatewayDevices(props.instanceId) }
  catch { error.value = t('connections.gatewayDevices.failed') }
  finally { busy.value = false }
}

async function change(action: 'approve' | 'reject' | 'remove', device: OpenClawGatewayDevice) {
  if (busy.value) return
  if (action !== 'reject' && !window.confirm(t(`connections.gatewayDevices.${action === 'approve' ? 'approveConfirm' : 'revokeConfirm'}`, { device: name(device), id: device.deviceId, access: access(device) }))) return
  busy.value = true
  error.value = ''
  try {
    await updateOpenClawGatewayDevice(props.instanceId, action, device)
    inventory.value = await getOpenClawGatewayDevices(props.instanceId)
  } catch {
    inventory.value = null
    error.value = t('connections.gatewayDevices.failed')
  } finally { busy.value = false }
}

onMounted(refresh)
</script>

<style scoped>
.gateway-devices { padding: 1.5rem; border: 1px solid var(--ag-border, #333); border-radius: 1rem; margin-block: 1.5rem; }
.gateway-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
.gateway-heading h2 { font-size: 1.2rem; font-weight: 600; }
.gateway-heading p { margin-block: .5rem 1rem; }
.gateway-devices h3 { margin-top: 1rem; font-weight: 600; }
.gateway-device { padding-block: 1rem; border-bottom: 1px solid var(--ag-border, #333); }
.gateway-device code { display: block; overflow-wrap: anywhere; font-size: .75rem; margin-block: .4rem; }
.gateway-device small { display: block; overflow-wrap: anywhere; }
.gateway-device-actions { display: flex; flex-wrap: wrap; gap: .75rem; margin-top: .75rem; }
@media (max-width: 640px) { .gateway-heading { flex-wrap: wrap; } }
</style>
