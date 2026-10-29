<template>
  <div data-testid="configured-channels">
    <p class="mb-4 leading-relaxed">管理视图展示服务器已配置的渠道、分组和模型映射。它不代表管理员个人密钥拥有这些组的权限；员工看到的是各自获授权的分组。</p>
    <p v-if="error" role="alert" class="ag-empty">配置读取失败。请刷新重试。</p>
    <p v-else-if="loading && !channels.length">正在读取渠道配置…</p>
    <p v-else-if="!filtered.length" class="ag-empty">{{ channels.length ? '没有符合搜索条件的渠道。' : '尚未配置渠道。' }}</p>
    <section v-for="channel in filtered" :key="channel.id" class="ag-channel card mb-4 p-4 md:p-6">
      <header class="flex flex-wrap items-baseline justify-between gap-3 border-b pb-4">
        <h2 class="text-2xl font-bold">{{ channel.name }}</h2>
        <span class="ag-status">{{ channel.status === 'active' ? '已启用' : '已停用' }}</span>
      </header>
      <p class="my-4 leading-relaxed">{{ channel.description || '暂无说明' }}</p>
      <div class="flex flex-wrap gap-3">
        <div v-for="group in groups.filter(g => channel.group_ids.includes(g.id))" :key="group.id" class="flex flex-wrap items-baseline gap-3 border p-3">
          <strong>{{ groupDisplayName(group.name, group.platform) }}</strong>
          <span>{{ group.platform }} · {{ group.rate_multiplier }}× · {{ group.status === 'active' ? '启用' : '停用' }}</span>
        </div>
      </div>
      <h3 class="mb-2 mt-4 font-semibold">已配置模型映射</h3>
      <ul class="grid gap-2 sm:grid-cols-2">
        <template v-for="(mapping, platform) in channel.model_mapping" :key="platform">
          <li v-for="(target, source) in mapping" :key="source" class="min-w-0 border p-3 text-sm">
            <span class="ag-model-id">{{ source }}</span><span v-if="source !== target"> → {{ target }}</span>
            <span class="ml-2">{{ platform }}</span>
          </li>
        </template>
      </ul>
      <p class="my-3 text-sm">{{ channel.restrict_models ? '该渠道限制模型范围。' : '映射表用于路由；它不是完整的可用模型白名单。' }} 实际可用性由账户权限与上游状态共同决定。</p>
      <RouterLink to="/admin/channels" class="btn btn-secondary">管理渠道配置</RouterLink>
    </section>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { list, type Channel } from '@/api/admin/channels'
import { getAllIncludingInactive } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import { groupDisplayName } from '@/utils/groupDisplayName'
const props = defineProps<{ search: string }>()
const channels = ref<Channel[]>([])
const groups = ref<AdminGroup[]>([])
const loading = ref(false)
const error = ref(false)
const filtered = computed(() => {
  const q = props.search.trim().toLowerCase()
  const order = (name: string) => { const index = ['通用', '编程', '推理'].indexOf(name); return index < 0 ? 3 : index }
  return channels.value.filter(c => !q || [c.name, c.description, ...groups.value.filter(g => c.group_ids.includes(g.id)).map(g => g.name), ...Object.values(c.model_mapping).flatMap(m => Object.keys(m))].join(' ').toLowerCase().includes(q)).sort((a, b) => order(a.name) - order(b.name) || a.name.localeCompare(b.name))
})
async function refresh() {
  if (loading.value) return
  loading.value = true
  try {
    const [first, allGroups] = await Promise.all([list(1, 100), getAllIncludingInactive()])
    const all = [...first.items]
    for (let page = 2; all.length < first.total; page++) {
      const next = await list(page, 100)
      if (!next.items.length) break
      all.push(...next.items)
    }
    channels.value = all
    groups.value = allGroups
    error.value = false
  } catch { error.value = true } finally { loading.value = false }
}
onMounted(refresh)
defineExpose({ refresh, loading })
</script>
