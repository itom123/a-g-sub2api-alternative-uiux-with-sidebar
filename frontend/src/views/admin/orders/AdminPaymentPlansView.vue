<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.subscriptionAccess.title', '订阅入口访问名单') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.subscriptionAccess.description') }}</p>
          </div>
          <button class="btn btn-primary" :disabled="accessSaving || !accessLoaded" @click="saveSubscriptionAccess">
            {{ accessSaving ? t('common.saving', '保存中…') : t('common.save', '保存') }}
          </button>
        </div>
        <div class="mt-3 flex gap-2">
          <input v-model="accessSearch" class="input min-w-0 flex-1" :placeholder="t('payment.admin.subscriptionAccess.search')" @keyup.enter="loadAccessUsers" />
          <button class="btn btn-secondary" :disabled="accessUsersLoading" @click="loadAccessUsers">{{ t('common.search') }}</button>
        </div>
        <div class="mt-3 max-h-56 divide-y divide-gray-200 overflow-y-auto dark:divide-dark-600">
          <label v-for="account in accessUsers" :key="account.id" class="flex cursor-pointer items-center gap-3 py-3">
            <input type="checkbox" :checked="accountAllowed(account)" :disabled="!accessLoaded" @change="toggleAccountAccess(account, ($event.target as HTMLInputElement).checked)" />
            <span class="min-w-0 text-sm"><span class="block font-medium">{{ account.username || account.email }}</span><span class="block truncate text-xs text-gray-500">{{ account.email }}</span></span>
          </label>
        </div>
        <p class="mt-2 text-xs text-gray-500">{{ t('payment.admin.subscriptionAccess.searchHint') }}</p>
        <p v-if="accessError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ accessError }}</p>
      </div>

      <!-- Actions -->
      <div class="flex items-center justify-end gap-2">
        <button @click="loadPlans" :disabled="plansLoading" class="btn btn-secondary" :title="t('common.refresh')">
          <Icon name="refresh" size="md" :class="plansLoading ? 'animate-spin' : ''" />
        </button>
        <button @click="openPlanEdit(null)" class="btn btn-primary">{{ t('payment.admin.createPlan') }}</button>
      </div>

      <!-- Plans Table -->
      <DataTable :columns="planColumns" :data="plans" :loading="plansLoading">
        <template #cell-name="{ value, row }">
          <span class="text-sm font-medium" :class="getPlanNameClass(row.group_id)">{{ value }}</span>
        </template>
        <template #cell-group_id="{ value }">
          <span v-if="isGroupMissing(value)" class="text-sm">
            <span class="text-gray-400">#{{ value }}</span>
            <span class="ml-1 badge badge-danger">{{ t('payment.admin.groupMissing') }}</span>
          </span>
          <GroupBadge
            v-else-if="getGroup(value)"
            :name="getGroup(value)!.name"
            :platform="getGroup(value)!.platform"
            :rate-multiplier="getGroup(value)!.rate_multiplier"
          />
          <span v-else class="text-sm text-gray-400">-</span>
        </template>
        <template #cell-price="{ value, row }">
          <div class="text-sm">
            <span class="font-medium text-gray-900 dark:text-white">{{ row.fixed_price_cny != null ? `¥${Number(row.fixed_price_cny).toFixed(2)}` : formatCny(value ?? 0) }}</span>
            <span class="ml-1 text-xs text-gray-400">CNY</span>
            <span v-if="row.original_price && row.fixed_price_cny == null" class="ml-1 text-xs text-gray-400 line-through">{{ formatCny(row.original_price) }}</span>
          </div>
        </template>
        <template #cell-validity_days="{ value, row }">
          <span class="text-sm">{{ value }} {{ t('payment.admin.' + (row.validity_unit || 'days')) }}</span>
        </template>
        <template #cell-for_sale="{ value, row }">
          <button
            type="button"
            :class="[
              'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              value ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'
            ]"
            @click="toggleForSale(row)"
          >
            <span :class="[
              'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
              value ? 'translate-x-4' : 'translate-x-0'
            ]" />
          </button>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-2">
            <button @click="openPlanEdit(row)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400">
              <Icon name="edit" size="sm" />
              <span class="text-xs">{{ t('common.edit') }}</span>
            </button>
            <button @click="confirmDeletePlan(row)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400">
              <Icon name="trash" size="sm" />
              <span class="text-xs">{{ t('common.delete') }}</span>
            </button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- Plan Edit Dialog -->
    <PlanEditDialog :show="showPlanDialog" :plan="editingPlan" :groups="groups" :payment-config="paymentConfig" @close="showPlanDialog = false" @saved="loadPlans" />

    <ConfirmDialog :show="showDeletePlanDialog" :title="t('payment.admin.deletePlan')" :message="t('payment.admin.deletePlanConfirm')" :confirm-text="t('common.delete')" danger @confirm="handleDeletePlan" @cancel="showDeletePlanDialog = false" />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AdminPaymentConfig } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import adminAPI from '@/api/admin'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminUser } from '@/types'
import type { AdminGroup } from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import PlanEditDialog from './PlanEditDialog.vue'
import { formatCny } from '@/utils/currency'
import { platformTextClass } from '@/utils/platformColors'

const { t } = useI18n()
const appStore = useAppStore()

// ==================== Groups ====================

const groups = ref<AdminGroup[]>([])
const paymentConfig = ref<AdminPaymentConfig | null>(null)
const subscriptionAllowlist = ref('')
const accessSaving = ref(false)
const accessError = ref('')
const accessLoaded = ref(false)
const accessSearch = ref('')
const accessUsers = ref<AdminUser[]>([])
const accessUsersLoading = ref(false)
function accessEntries(): string[] {
  return subscriptionAllowlist.value.split(/[,;\n]/).map(s => s.trim()).filter(Boolean)
}
function accountAllowed(account: AdminUser): boolean {
  return accessEntries().some(entry => entry === String(account.id) || entry.toLowerCase() === account.email.toLowerCase())
}
function toggleAccountAccess(account: AdminUser, enabled: boolean) {
  const entries = accessEntries().filter(entry => entry !== String(account.id) && entry.toLowerCase() !== account.email.toLowerCase())
  if (enabled) entries.push(String(account.id))
  subscriptionAllowlist.value = entries.join('\n')
}
async function loadAccessUsers() {
  accessUsersLoading.value = true
  try {
    const result = await adminAPI.users.list(1, 20, { role: 'user', search: accessSearch.value })
    accessUsers.value = result.items
  } catch (err: unknown) {
    accessError.value = extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))
  } finally { accessUsersLoading.value = false }
}

async function loadGroups() {
  try {
    groups.value = await adminAPI.groups.getAll()
  } catch { /* ignore */ }
}

async function loadPaymentConfig() {
  try {
    const res = await adminPaymentAPI.getConfig()
    paymentConfig.value = res.data
    subscriptionAllowlist.value = res.data.subscription_sidebar_allowlist || ''
    accessLoaded.value = true
  } catch (err: unknown) { accessError.value = extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')) }
}

async function saveSubscriptionAccess() {
  accessSaving.value = true
  accessError.value = ''
  try {
    await adminPaymentAPI.updateConfig({ subscription_sidebar_allowlist: subscriptionAllowlist.value })
    appStore.showSuccess(t('common.saved', '已保存'))
  } catch (err: unknown) {
    accessError.value = extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))
  } finally {
    accessSaving.value = false
  }
}

function getGroup(id: number): AdminGroup | undefined {
  return groups.value.find(g => g.id === id)
}

function isGroupMissing(id: number): boolean {
  return id > 0 && !groups.value.find(g => g.id === id)
}

function getPlanNameClass(groupId: number): string {
  const group = getGroup(groupId)
  return group ? platformTextClass(group.platform) : 'text-gray-900 dark:text-white'
}


// ==================== Plans ====================

const plansLoading = ref(false)
const plans = ref<SubscriptionPlan[]>([])
const showPlanDialog = ref(false)
const showDeletePlanDialog = ref(false)
const editingPlan = ref<SubscriptionPlan | null>(null)
const deletingPlanId = ref<number | null>(null)

const planColumns = computed((): Column[] => [
  { key: 'id', label: 'ID' },
  { key: 'name', label: t('payment.admin.planName') },
  { key: 'group_id', label: t('payment.admin.group') },
  { key: 'price', label: t('payment.admin.price') },
  { key: 'validity_days', label: t('payment.admin.validity') },
  { key: 'for_sale', label: t('payment.admin.forSale') },
  { key: 'sort_order', label: t('payment.admin.sortOrder') },
  { key: 'actions', label: t('common.actions') },
])

async function loadPlans() {
  plansLoading.value = true
  try {
    const res = await adminPaymentAPI.getPlans()
    // Backend returns features as newline-separated string; parse to array
    plans.value = (res.data || []).map((p: Omit<SubscriptionPlan, 'features'> & { features: string | string[] }) => ({
      ...p,
      features: typeof p.features === 'string'
        ? p.features.split('\n').map((f: string) => f.trim()).filter(Boolean)
        : (p.features || []),
    }))
  }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally { plansLoading.value = false }
}

function openPlanEdit(plan: SubscriptionPlan | null) {
  editingPlan.value = plan
  showPlanDialog.value = true
}


/** Quick toggle for_sale from the list */
async function toggleForSale(plan: SubscriptionPlan) {
  try {
    await adminPaymentAPI.updatePlan(plan.id, { for_sale: !plan.for_sale })
    plan.for_sale = !plan.for_sale
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  }
}

function confirmDeletePlan(plan: SubscriptionPlan) { deletingPlanId.value = plan.id; showDeletePlanDialog.value = true }
async function handleDeletePlan() {
  if (!deletingPlanId.value) return
  try { await adminPaymentAPI.deletePlan(deletingPlanId.value); appStore.showSuccess(t('common.deleted')); showDeletePlanDialog.value = false; loadPlans() }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
}

// ==================== Lifecycle ====================

onMounted(() => {
  loadGroups()
  loadPaymentConfig()
  loadAccessUsers()
  loadPlans()
})
</script>
