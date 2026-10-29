<template>
  <div class="ag-freshness" role="status" aria-live="polite">
    <template v-if="status.snapshot && status.hasObservation">
      <span>观测截至 {{ stamp(status.snapshot.coverage.data_through) }}</span>
      <span v-if="status.stale" class="ag-status ag-status-warning">数据更新延迟{{ status.statusError ? '：刷新失败' : '' }}</span>
      <span v-if="!status.snapshot.coverage.coverage_complete || status.snapshot.coverage.bootstrap?.active" class="ag-status">数据补齐中<span v-if="status.snapshot.coverage.bootstrap?.active"> · {{ Math.round(status.snapshot.coverage.bootstrap.progress_percent) }}%</span></span>
      <details><summary>数据说明</summary><p>计算时间 {{ stamp(status.snapshot.coverage.computed_at) }}；最低样本 {{ status.snapshot.health.minimum_sample }} 次。用户端不公开请求及延迟样本数量，数值零不能据此判断无流量。状态依据实际请求，非主动探测；全模型标签不代表单一分组。{{ status.interval }} 秒自动刷新。</p></details>
    </template>
    <span v-else-if="status.loading">正在获取观测数据…</span>
    <span v-else>{{ status.statusError ? '无可用数据：状态获取失败，请刷新重试。' : status.monitorEnabled ? '无可用数据：当前账户暂无可观测数据。' : '实时状态未启用。' }}</span>
    <span v-if="!status.hasObservation && status.snapshot?.coverage.bootstrap?.active" class="ag-status">数据补齐中 · {{ Math.round(status.snapshot.coverage.bootstrap.progress_percent) }}%</span>
  </div>
</template>
<script setup lang="ts">
import { useModelStatusStore } from '@/stores/modelStatus'
const status = useModelStatusStore()
function stamp(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '暂无时间' : new Intl.DateTimeFormat(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short' }).format(date)
}
</script>
