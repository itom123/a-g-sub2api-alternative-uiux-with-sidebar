<template>
  <BaseDialog
    :show="Boolean(displayedAnnouncement)"
    :title="displayedAnnouncement?.title || ''"
    width="wide"
    :z-index="120"
    :show-close-button="preview"
    :close-on-escape="preview"
    :close-on-click-outside="false"
    @close="handleDismiss"
  >
    <template v-if="displayedAnnouncement">
      <div class="ag-announcement-meta">
        <span class="ag-status">{{ t('announcements.unread') }}</span>
        <time :datetime="displayedAnnouncement.created_at">{{ formatRelativeWithDateTime(displayedAnnouncement.created_at) }}</time>
      </div>
      <div class="markdown-body" v-html="renderedContent"></div>
    </template>
    <template #footer>
      <button class="btn btn-primary" data-testid="announcement-popup-dismiss" @click="handleDismiss">
        {{ preview ? t('common.close') : t('announcements.markRead') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import '@/styles/announcement-markdown.css'
import BaseDialog from './BaseDialog.vue'

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>

const props = withDefaults(defineProps<{
  announcement?: PreviewAnnouncement | null
  preview?: boolean
}>(), {
  announcement: null,
  preview: false,
})

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const announcementStore = useAnnouncementStore()
const displayedAnnouncement = computed(() => (
  props.preview ? props.announcement : announcementStore.currentPopup
))

marked.setOptions({
  breaks: true,
  gfm: true,
})

const renderedContent = computed(() => {
  const content = displayedAnnouncement.value?.content
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
})

function handleDismiss() {
  if (props.preview) {
    emit('close')
    return
  }
  announcementStore.dismissPopup()
}

</script>
