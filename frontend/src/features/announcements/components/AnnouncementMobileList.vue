<script setup lang="ts">
import {
  Archive,
  BarChart3,
  Eye,
  Pencil,
  Pin,
  Send,
  Trash2,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import type { AdminAnnouncement } from '@/api/announcements'
import EmptyState from '@/components/common/EmptyState.vue'
import IconButton from '@/components/common/IconButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import {
  announcementExcerpt,
  announcementLiveState,
  announcementStatusTone,
  announcementTypeTone,
  formatUnixDateTime,
} from '@/features/announcements/lib/announcementHelpers'

defineProps<{
  rows: AdminAnnouncement[]
  loading: boolean
  canWrite: boolean
  busyIds?: ReadonlySet<number>
  nowSec: number
}>()

const emit = defineEmits<{
  preview: [row: AdminAnnouncement]
  readStatus: [row: AdminAnnouncement]
  toggleStatus: [row: AdminAnnouncement]
  edit: [row: AdminAnnouncement]
  delete: [row: AdminAnnouncement]
}>()

const { t, locale } = useI18n()
</script>

<template>
  <div class="p-3 lg:hidden" data-announcement-mobile-list>
    <div v-if="loading && !rows.length" class="space-y-3" aria-busy="true">
      <SkeletonBlock v-for="i in 3" :key="i" bordered class="h-32" />
    </div>

    <EmptyState
      v-else-if="!rows.length"
      :title="t('announcements.admin.emptyTitle')"
      :hint="t('announcements.admin.emptyHint')"
    />

    <ul v-else class="space-y-3">
      <li
        v-for="row in rows"
        :key="row.id"
        data-announcement-mobile-row
        class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3.5"
      >
        <div class="flex flex-wrap items-center gap-1.5">
          <StatusChip :tone="announcementTypeTone(row.type)">
            {{ t(`announcements.types.${row.type}`) }}
          </StatusChip>
          <StatusChip :tone="announcementStatusTone(row.status)">
            {{ t(`announcements.statuses.${row.status}`) }}
          </StatusChip>
          <StatusChip v-if="announcementLiveState(row, nowSec)" tone="warning">
            {{
              t(
                `announcements.liveStates.${announcementLiveState(row, nowSec)}`
              )
            }}
          </StatusChip>
          <StatusChip
            :tone="row.notify_mode === 'popup' ? 'accent' : 'neutral'"
          >
            {{ t(`announcements.notifyModes.${row.notify_mode}`) }}
          </StatusChip>
          <StatusChip v-if="row.pinned" tone="accent">
            <Pin :size="11" aria-hidden="true" />
            {{ t('announcements.pinned') }}
          </StatusChip>
        </div>

        <h3
          class="mt-2 break-words text-sm font-semibold text-[var(--text-primary)]"
        >
          {{ row.title }}
        </h3>
        <p class="mt-1 line-clamp-2 text-xs text-[var(--text-secondary)]">
          {{ announcementExcerpt(row.content) }}
        </p>

        <div
          class="mt-2.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--text-tertiary)]"
        >
          <span>#{{ row.id }}</span>
          <span>{{ formatUnixDateTime(row.created_at, locale) }}</span>
          <button
            type="button"
            class="inline-flex min-h-11 items-center font-medium text-[var(--accent-text)] hover:underline sm:min-h-0"
            @click="emit('readStatus', row)"
          >
            {{ t('announcements.admin.colReadCount') }}: {{ row.read_count }}
          </button>
        </div>

        <div
          class="mt-3 flex flex-wrap items-center justify-end gap-1 border-t border-[var(--border-subtle)] pt-2.5"
        >
          <IconButton
            :label="t('announcements.admin.preview')"
            @click="emit('preview', row)"
          >
            <Eye :size="16" aria-hidden="true" />
          </IconButton>
          <IconButton
            :label="t('announcements.admin.readStatus')"
            @click="emit('readStatus', row)"
          >
            <BarChart3 :size="16" aria-hidden="true" />
          </IconButton>
          <template v-if="canWrite">
            <IconButton
              :label="
                row.status === 'active'
                  ? t('announcements.admin.archive')
                  : t('announcements.admin.publish')
              "
              :disabled="busyIds?.has(row.id) ?? false"
              @click="emit('toggleStatus', row)"
            >
              <Archive
                v-if="row.status === 'active'"
                :size="16"
                aria-hidden="true"
              />
              <Send v-else :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              :label="t('announcements.admin.edit')"
              @click="emit('edit', row)"
            >
              <Pencil :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              tone="danger"
              :label="t('announcements.admin.delete')"
              @click="emit('delete', row)"
            >
              <Trash2 :size="16" aria-hidden="true" />
            </IconButton>
          </template>
        </div>
      </li>
    </ul>
  </div>
</template>
