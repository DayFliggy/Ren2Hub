<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import {
  adminAnnouncementsApi,
  type AdminAnnouncement,
  type AnnouncementReadFilter,
  type AnnouncementUserReadStatus,
} from '@/api/announcements'
import { ApiError } from '@/api/types'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { formatUnixDateTime } from '@/features/announcements/lib/announcementHelpers'

const props = defineProps<{
  open: boolean
  announcement: AdminAnnouncement | null
}>()

const emit = defineEmits<{
  close: []
}>()

const { t, locale } = useI18n()
const request = useLatestRequest()

const rows = ref<AnnouncementUserReadStatus[]>([])
const total = ref(0)
const totalUsers = ref(0)
const readCount = ref(0)
const page = ref(1)
const pageSize = ref(10)
const keyword = ref('')
const filter = ref<AnnouncementReadFilter>('')
const loading = ref(false)
const error = ref('')

let searchTimer = 0
let resetting = false

const readPercent = computed(() => {
  if (totalUsers.value <= 0) return 0
  return Math.min(100, Math.round((readCount.value / totalUsers.value) * 100))
})

const filterOptions = computed(() => [
  { value: '', label: t('announcements.admin.reads.filterAll') },
  { value: 'read', label: t('announcements.admin.reads.filterRead') },
  { value: 'unread', label: t('announcements.admin.reads.filterUnread') },
])

const columns = computed<TableColumn[]>(() => [
  { key: 'user', label: t('announcements.admin.reads.colUser') },
  { key: 'email', label: t('announcements.admin.reads.colEmail') },
  {
    key: 'state',
    label: t('announcements.admin.reads.colState'),
    width: '104px',
  },
  {
    key: 'read_at',
    label: t('announcements.admin.reads.colReadAt'),
    width: '176px',
  },
])

async function load() {
  if (!props.open || !props.announcement) return
  loading.value = true
  error.value = ''
  const result = await request.run((signal) =>
    adminAnnouncementsApi.readStatus(
      props.announcement!.id,
      {
        page: page.value,
        page_size: pageSize.value,
        keyword: keyword.value,
        filter: filter.value,
      },
      signal
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    rows.value = []
    total.value = 0
    error.value =
      result.error instanceof ApiError
        ? result.error.message
        : String(result.error)
    return
  }
  rows.value = result.value.items
  total.value = result.value.total
  totalUsers.value = result.value.total_users
  readCount.value = result.value.read_count
}

function reloadFromFirstPage() {
  window.clearTimeout(searchTimer)
  if (page.value === 1) void load()
  else page.value = 1
}

function clearState() {
  window.clearTimeout(searchTimer)
  request.cancel()
  rows.value = []
  total.value = 0
  totalUsers.value = 0
  readCount.value = 0
  error.value = ''
  loading.value = false
}

watch(
  () => [props.open, props.announcement?.id] as const,
  ([isOpen]) => {
    clearState()
    if (!isOpen) return
    resetting = true
    keyword.value = ''
    filter.value = ''
    page.value = 1
    resetting = false
    void load()
  }
)

watch(keyword, () => {
  if (!props.open || resetting) return
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reloadFromFirstPage, 250)
})

watch([filter, pageSize], () => {
  if (!props.open || resetting) return
  reloadFromFirstPage()
})

watch(page, () => {
  if (!props.open || resetting) return
  void load()
})

onBeforeUnmount(() => {
  clearState()
})
</script>

<template>
  <ConsoleModal
    :open="open && announcement !== null"
    :title="t('announcements.admin.reads.title')"
    :subtitle="announcement?.title"
    size="xl"
    @close="emit('close')"
  >
    <div class="space-y-4" data-announcement-read-status-modal>
      <div
        class="space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3.5"
      >
        <div class="flex items-center justify-between gap-2 text-xs">
          <span class="font-medium text-[var(--text-primary)]">
            {{
              t('announcements.admin.reads.summary', {
                read: readCount,
                total: totalUsers,
                percent: readPercent,
              })
            }}
          </span>
        </div>
        <div
          class="h-2 overflow-hidden rounded-full bg-[var(--studio-control-track,var(--surface-muted))]"
          role="progressbar"
          :aria-valuenow="readPercent"
          aria-valuemin="0"
          aria-valuemax="100"
        >
          <div
            class="h-full rounded-full bg-[var(--accent)] transition-all"
            :style="{ width: `${readPercent}%` }"
          />
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2.5">
        <SearchInput
          v-model="keyword"
          :placeholder="t('announcements.admin.reads.searchPlaceholder')"
          :aria-label="t('announcements.admin.reads.searchPlaceholder')"
          size="compact"
          class="w-full sm:w-64"
        />
        <SegmentedToggle
          v-model="filter"
          :options="filterOptions"
          :label="t('announcements.admin.reads.colState')"
        />
        <IconButton
          class="ml-auto"
          :label="t('announcements.admin.refresh')"
          :disabled="loading"
          @click="load()"
        >
          <RefreshCw :size="15" :class="loading ? 'animate-spin' : ''" />
        </IconButton>
      </div>

      <ErrorBanner v-if="error" :message="error" />

      <div class="hidden md:block">
        <DataTable
          :columns="columns"
          :rows="rows"
          row-key="user_id"
          :loading="loading"
          :skeleton-rows="5"
          min-table-width="580px"
          :empty-title="t('announcements.admin.reads.emptyTitle')"
          :empty-hint="t('announcements.admin.reads.emptyHint')"
        >
          <template #cell-user="{ row }">
            <div class="min-w-0">
              <p
                class="truncate text-sm font-medium text-[var(--text-primary)]"
              >
                {{
                  (row as AnnouncementUserReadStatus).display_name ||
                  (row as AnnouncementUserReadStatus).username
                }}
              </p>
              <p class="truncate text-xs text-[var(--text-tertiary)]">
                @{{ (row as AnnouncementUserReadStatus).username }}
              </p>
            </div>
          </template>

          <template #cell-email="{ row }">
            <span class="block truncate text-xs text-[var(--text-secondary)]">
              {{ (row as AnnouncementUserReadStatus).email || '—' }}
            </span>
          </template>

          <template #cell-state="{ row }">
            <StatusChip
              :tone="
                (row as AnnouncementUserReadStatus).read_at
                  ? 'success'
                  : 'neutral'
              "
            >
              {{
                (row as AnnouncementUserReadStatus).read_at
                  ? t('announcements.read')
                  : t('announcements.unread')
              }}
            </StatusChip>
          </template>

          <template #cell-read_at="{ row }">
            <span class="text-xs tabular-nums text-[var(--text-secondary)]">
              {{
                (row as AnnouncementUserReadStatus).read_at
                  ? formatUnixDateTime(
                      (row as AnnouncementUserReadStatus).read_at!,
                      locale
                    )
                  : '—'
              }}
            </span>
          </template>
        </DataTable>
      </div>

      <div class="space-y-2 md:hidden">
        <template v-if="loading && !rows.length">
          <SkeletonBlock v-for="i in 3" :key="i" bordered class="h-16" />
        </template>
        <EmptyState
          v-else-if="!rows.length"
          :title="t('announcements.admin.reads.emptyTitle')"
          :hint="t('announcements.admin.reads.emptyHint')"
        />
        <ul v-else class="space-y-2">
          <li
            v-for="row in rows"
            :key="row.user_id"
            class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3"
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <p
                  class="truncate text-sm font-medium text-[var(--text-primary)]"
                >
                  {{ row.display_name || row.username }}
                </p>
                <p class="truncate text-xs text-[var(--text-tertiary)]">
                  @{{ row.username }}
                  <span v-if="row.email"> · {{ row.email }}</span>
                </p>
              </div>
              <StatusChip :tone="row.read_at ? 'success' : 'neutral'">
                {{
                  row.read_at
                    ? t('announcements.read')
                    : t('announcements.unread')
                }}
              </StatusChip>
            </div>
            <p
              v-if="row.read_at"
              class="mt-1.5 text-xs tabular-nums text-[var(--text-tertiary)]"
            >
              {{ formatUnixDateTime(row.read_at, locale) }}
            </p>
          </li>
        </ul>
      </div>

      <TablePagination
        v-if="total > 0"
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
      />
    </div>
  </ConsoleModal>
</template>
