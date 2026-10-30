<template>
  <div class="ag-model-health">
    <span class="ag-status" :class="`ag-status-${state}`" :title="observationTitle">{{ label }}</span>
    <small v-if="!compact">{{ details }}</small>
    <RouterLink v-if="groupId" :to="{ path: '/monitor', query: { group: String(groupId), platform, model: `${platform}\u0000${model}` } }" class="text-xs underline underline-offset-4">本组观测</RouterLink>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useModelStatusStore } from '@/stores/modelStatus'
const props = defineProps<{ platform: string; model: string; compact?: boolean; groupId?: number }>()
const status = useModelStatusStore()
const row = computed(() => status.findModel(props.platform, props.model))
const state = computed(() => status.stale || status.statusError ? 'unknown' : row.value?.health.overall || 'unknown')
const observationTitle = computed(() => `${details.value}${status.hasObservation && status.snapshot ? ' · 观测截至 ' + new Date(status.snapshot.coverage.data_through).toLocaleString(undefined, { timeZoneName: 'short' }) : ''}`)
const label = computed(() => ({ healthy: '正常', warning: '性能下降', critical: '异常', unknown: '待观测' })[state.value])
const details = computed(() => {
  if (!status.monitorEnabled) return '实时状态未启用'
  if (status.statusError) return '状态获取失败，暂不判定健康状态'
  if (status.stale) return '观测数据更新延迟，暂不判定健康状态'
  if (!row.value) return '暂无此模型的近期观测记录'
  const r = row.value
  const latency = r.metrics.ttft.p50_ms == null ? '首字延迟暂无样本' : `首字 P50 ${(r.metrics.ttft.p50_ms / 1000).toFixed(2)} 秒`
  const errors = typeof r.metrics.error_rate === 'number' ? ` · 错误率 ${(r.metrics.error_rate * 100).toFixed(1)}%` : ''
  return `${status.filter.groupIds.length ? '所选分组观测' : '全模型观测'} · 样本数未公开${r.health.overall === 'unknown' ? `，最低观测样本 ${r.health.minimum_sample} 次` : ''} · ${latency}${errors}`
})
</script>
