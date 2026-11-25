<script setup lang="ts">
import { computed } from 'vue'
import { Check, Clock, Pin, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import type { AnnouncementBase, UserAnnouncement } from '@/api/announcements'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import IconButton from '@/components/common/IconButton.vue'
import MarkdownDocument from '@/components/common/MarkdownDocument.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import {
  announcementTypeTone,
  formatUnixDateTime,
} from '@/features/announcements/lib/announcementHelpers'

type DetailItem = AnnouncementBase & { read_at?: number | null }

const props = withDefaults(
  defineProps<{
    open: boolean
    announcement: DetailItem | null
    preview?: boolean
    popup?: boolean
    zIndex?: number
  }>(),
  {
    preview: false,
    popup: false,
    zIndex: undefined,
  }
)

const emit = defineEmits<{
  close: []
  markRead: [id: number]
}>()

const { t, locale } = useI18n()

const isUnread = computed(() => {
  if (!props.announcement || props.preview) return false
  return (props.announcement as UserAnnouncement).read_at === null
})

const publishedTime = computed(() =>
  props.announcement
    ? formatUnixDateTime(props.announcement.created_at, locale.value)
    : ''
)

const readAtTime = computed(() => {
  const readAt = (props.announcement as UserAnnouncement | null)?.read_at
  return readAt ? formatUnixDateTime(readAt, locale.value) : ''
})

const scheduleSummary = computed(() => {
  if (!props.announcement) return ''
  if (
    props.announcement.starts_at === null &&
    props.announcement.ends_at === null
  ) {
    return ''
  }
  const start =
    props.announcement.starts_at !== null
      ? formatUnixDateTime(props.announcement.starts_at, locale.value)
      : t('announcements.timeImmediate')
  const end =
    props.announcement.ends_at !== null
      ? formatUnixDateTime(props.announcement.ends_at, locale.value)
      : t('announcements.timeNever')
  return t('announcements.scheduleWindow', { start, end })
})
</script>

<template>
  <ConsoleModal
    :open="open && announcement !== null"
    :aria-label="announcement?.title || t('announcements.title')"
    size="lg"
    :z-index="zIndex"
    @close="emit('close')"
  >
    <template v-if="announcement" #header>
      <div class="flex items-start justify-between gap-3 text-left">
        <div class="min-w-0 flex-1 space-y-2.5">
          <div class="flex flex-wrap items-center gap-2">
            <StatusChip :tone="announcementTypeTone(announcement.type)">
              {{ t(`announcements.types.${announcement.type}`) }}
            </StatusChip>
            <StatusChip v-if="announcement.pinned" tone="accent">
              <Pin :size="12" aria-hidden="true" />
              {{ t('announcements.pinned') }}
            </StatusChip>
            <StatusChip v-if="popup && !preview" tone="warning">
              {{ t('announcements.popupNotice') }}
            </StatusChip>
            <StatusChip v-if="preview" tone="info">
              {{ t('announcements.previewMode') }}
            </StatusChip>
            <StatusChip v-else-if="isUnread" tone="accent">
              {{ t('announcements.unread') }}
            </StatusChip>
          </div>

          <h2
            class="display-title break-words text-xl font-bold leading-snug text-[var(--text-primary)]"
            data-announcement-detail-title
          >
            {{ announcement.title }}
          </h2>

          <div
            class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-[var(--text-tertiary)]"
          >
            <span class="inline-flex items-center gap-1.5">
              <Clock :size="13" aria-hidden="true" />
              {{ t('announcements.publishedAt', { time: publishedTime }) }}
            </span>
            <span v-if="scheduleSummary">{{ scheduleSummary }}</span>
            <span v-if="!preview && readAtTime">
              {{ t('announcements.readStatusHint', { time: readAtTime }) }}
            </span>
          </div>
        </div>
        <IconButton
          v-if="!popup || preview"
          class="shrink-0"
          :label="t('common.close')"
          @click="emit('close')"
        >
          <X :size="16" aria-hidden="true" />
        </IconButton>
      </div>
    </template>

    <div
      v-if="announcement"
      class="announcement-detail-body"
      data-announcement-detail-body
    >
      <MarkdownDocument :markdown="announcement.content" />
    </div>

    <template v-if="announcement" #footer>
      <div class="flex flex-wrap items-center justify-end gap-2.5">
        <ConsoleButton
          v-if="popup && !preview"
          data-announcement-popup-dismiss
          @click="emit('close')"
        >
          <Check :size="15" aria-hidden="true" />
          {{ t('announcements.acknowledge') }}
        </ConsoleButton>
        <template v-else>
          <ConsoleButton
            v-if="isUnread"
            variant="primary"
            data-announcement-mark-read
            @click="emit('markRead', announcement.id)"
          >
            <Check :size="15" aria-hidden="true" />
            {{ t('announcements.markRead') }}
          </ConsoleButton>
          <ConsoleButton variant="secondary" @click="emit('close')">
            {{ t('common.close') }}
          </ConsoleButton>
        </template>
      </div>
    </template>
  </ConsoleModal>
</template>

<style scoped>
.announcement-detail-body {
  padding-left: 1rem;
  border-left: 2px solid var(--accent);
  color: var(--text-primary);
  line-height: 1.7;
}
</style>
