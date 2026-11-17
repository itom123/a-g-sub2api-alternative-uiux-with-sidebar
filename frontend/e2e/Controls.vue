<template>
  <main class="mx-auto max-w-2xl space-y-6 p-4">
    <h1>Shared controls</h1>
    <Input id="name" v-model="name" label="Name" placeholder="Enter a name" />
    <Input id="invalid" model-value="" label="Required name" error="Enter your name" />
    <Input id="disabled" model-value="Unavailable" label="Disabled field" disabled />
    <TextArea id="notes" v-model="notes" label="Notes" placeholder="Additional context" />
    <Select v-model="choice" :options="options" searchable />
    <Select v-model="group" :options="groupOptions" aria-label="Group" searchable>
      <template #selected="{ option }">
        <GroupBadge v-if="option" :name="String(option.label)" platform="anthropic" :rate-multiplier="Number(option.rate)" />
      </template>
      <template #option="{ option, selected }">
        <GroupOptionItem :name="String(option.label)" platform="anthropic" :rate-multiplier="Number(option.rate)" :description="String(option.description)" :selected="selected" peak-rate-enabled peak-start="20:00" peak-end="23:00" :peak-rate-multiplier="1.5" />
      </template>
    </Select>
    <Toggle v-model="enabled" />
    <DateRangePicker start-date="2026-01-01" end-date="2026-01-07" />
    <button class="btn btn-primary" @click="show = true"><span class="text-sm bg-transparent"><Icon name="plus" size="sm" /> Open dialog</span></button>
    <button class="btn btn-secondary" @click="legacy = true">Open legacy dialog</button>
    <button class="btn btn-secondary" @click="store.showToast('error', 'Example notification that wraps on a narrow phone.')">Show notification</button>
    <HelpTooltip content="Details remain readable in either theme." trigger="click"><template #trigger><span class="btn btn-secondary">Help</span></template></HelpTooltip>
    <BaseDialog :show="show" title="A dialog with a long title that wraps on a narrow phone" @close="show = false">
      <p>Readable dialog content and nested action text.</p>
      <Input id="dialog-name" v-model="name" label="Dialog name" />
      <template #footer><button class="btn btn-primary" @click="show = false"><span class="text-sm">Done</span></button></template>
    </BaseDialog>
    <div v-if="legacy" data-testid="legacy-scrim" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label="Legacy dialog">
      <div class="card w-full max-w-md space-y-4 p-4">
        <p>Legacy dialog content</p>
        <button class="btn btn-primary" @click="legacy = false">Close legacy dialog</button>
      </div>
    </div>
    <Toast />
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import Input from '../src/components/common/Input.vue'
import TextArea from '../src/components/common/TextArea.vue'
import Select from '../src/components/common/Select.vue'
import Toggle from '../src/components/common/Toggle.vue'
import GroupBadge from '../src/components/common/GroupBadge.vue'
import GroupOptionItem from '../src/components/common/GroupOptionItem.vue'
import DateRangePicker from '../src/components/common/DateRangePicker.vue'
import BaseDialog from '../src/components/common/BaseDialog.vue'
import HelpTooltip from '../src/components/common/HelpTooltip.vue'
import Icon from '../src/components/icons/Icon.vue'
import Toast from '../src/components/common/Toast.vue'
import { useAppStore } from '../src/stores/app'

const name = ref('Theme verification')
const notes = ref('A readable text area')
const choice = ref('one')
const group = ref(1)
const enabled = ref(false)
const show = ref(false)
const legacy = ref(false)
const store = useAppStore()
const options = [{ value: 'one', label: 'First option' }, { value: 'two', label: 'Second option' }]
const groupOptions = [
  { value: 1, label: 'Primary group', rate: 0.25, description: 'Selected group description.' },
  { value: 2, label: 'Secondary group', rate: 0.2, description: 'Another group description.' },
]
</script>
