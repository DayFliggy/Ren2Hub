<script setup lang="ts">
import {
  Archive,
  BarChart3,
  Eye,
  Pencil,
  Pin,
  Plus,
  RefreshCw,
  Send,
  Trash2,
  X,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  ANNOUNCEMENT_STATUSES,
  ANNOUNCEMENT_TYPES,
  adminAnnouncementsApi,
  type AdminAnnouncement,
  type AnnouncementStatus,
  type AnnouncementType,
  type AnnouncementWriteInput,
} from '@/api/announcements'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import AnnouncementDetailModal from '@/components/console/announcements/AnnouncementDetailModal.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import AnnouncementFormModal from '@/features/announcements/components/AnnouncementFormModal.vue'
import AnnouncementMobileList from '@/features/announcements/components/AnnouncementMobileList.vue'
import AnnouncementReadStatusModal from '@/features/announcements/components/AnnouncementReadStatusModal.vue'
import {
  announcementExcerpt,
  announcementLiveState,
  announcementStatusTone,
  announcementTypeTone,
  formatUnixDateTime,
} from '@/features/announcements/lib/announcementHelpers'
import { useAnnouncementStore } from '@/stores/announcements'
import { useAuthStore } from '@/stores/auth'

const { t, locale } = useI18n()
const toast = useToast()
const auth = useAuthStore()
const announcementStore = useAnnouncementStore()
const listRequest = useLatestRequest()

const canWrite = computed(() => auth.hasPermission('announcement', 'write'))

const rows = ref<AdminAnnouncement[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const keyword = ref('')
const statusFilter = ref<AnnouncementStatus | ''>('')
const typeFilter = ref<AnnouncementType | ''>('')
const nowSec = ref(Math.floor(Date.now() / 1000))

const formOpen = ref(false)
const editing = ref<AdminAnnouncement | null>(null)
const formSaving = ref(false)
const formError = ref('')

const previewing = ref<AdminAnnouncement | null>(null)
const readStatusTarget = ref<AdminAnnouncement | null>(null)
const deleting = ref<AdminAnnouncement | null>(null)
const deleteLoading = ref(false)
const busyIds = ref<Set<number>>(new Set())

let searchTimer = 0

const hasFilters = computed(
  () =>
    keyword.value.trim() !== '' ||
    statusFilter.value !== '' ||
    typeFilter.value !== ''
)

const statusOptions = computed(() => [
  { value: '', label: t('announcements.admin.allStatuses') },
  ...ANNOUNCEMENT_STATUSES.map((value) => ({
    value,
    label: t(`announcements.statuses.${value}`),
  })),
])

const typeOptions = computed(() => [
  { value: '', label: t('announcements.admin.allTypes') },
  ...ANNOUNCEMENT_TYPES.map((value) => ({
    value,
    label: t(`announcements.types.${value}`),
  })),
])

const columns = computed<TableColumn[]>(() => [
  { key: 'title', label: t('announcements.admin.colTitle') },
  {
    key: 'type',
    label: t('announcements.admin.colType'),
    width: '96px',
  },
  {
    key: 'status',
    label: t('announcements.admin.colStatus'),
    width: '132px',
  },
  {
    key: 'notify_mode',
    label: t('announcements.admin.colNotifyMode'),
    width: '112px',
  },
  {
    key: 'schedule',
    label: t('announcements.admin.colSchedule'),
    width: '188px',
  },
  {
    key: 'read_count',
    label: t('announcements.admin.colReadCount'),
    width: '96px',
    align: 'right',
  },
  {
    key: 'actions',
    label: t('announcements.admin.colActions'),
    width: canWrite.value ? '220px' : '104px',
    align: 'right',
  },
])

async function load() {
  loading.value = true
  const result = await listRequest.run((signal) =>
    adminAnnouncementsApi.list(
      {
        page: page.value,
        page_size: pageSize.value,
        keyword: keyword.value,
        status: statusFilter.value,
        type: typeFilter.value,
      },
      signal
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    rows.value = []
    total.value = 0
    toast.error(
      result.error instanceof ApiError
        ? result.error.message
        : String(result.error)
    )
    return
  }
  nowSec.value = Math.floor(Date.now() / 1000)
  rows.value = result.value.items
  total.value = result.value.total
}

function reloadFromFirstPage() {
  window.clearTimeout(searchTimer)
  if (page.value === 1) void load()
  else page.value = 1
}

function resetFilters() {
  keyword.value = ''
  statusFilter.value = ''
  typeFilter.value = ''
  reloadFromFirstPage()
}

function openCreate() {
  if (!canWrite.value) return
  editing.value = null
  formError.value = ''
  formOpen.value = true
}

function openEdit(row: AdminAnnouncement) {
  if (!canWrite.value) return
  editing.value = row
  formError.value = ''
  formOpen.value = true
}

function closeForm() {
  if (formSaving.value) return
  formOpen.value = false
}

async function handleSave(payload: AnnouncementWriteInput) {
  if (!canWrite.value || formSaving.value) return
  formSaving.value = true
  formError.value = ''
  try {
    if (editing.value) {
      await adminAnnouncementsApi.update(editing.value.id, payload)
      toast.success(t('announcements.admin.updated'))
    } else {
      await adminAnnouncementsApi.create(payload)
      toast.success(t('announcements.admin.created'))
    }
    formOpen.value = false
    await load()
    void announcementStore.fetchAnnouncements(true)
  } catch (error) {
    formError.value = error instanceof ApiError ? error.message : String(error)
  } finally {
    formSaving.value = false
  }
}

async function toggleStatus(row: AdminAnnouncement) {
  if (!canWrite.value || busyIds.value.has(row.id)) return
  const nextStatus: AnnouncementStatus =
    row.status === 'active' ? 'archived' : 'active'
  busyIds.value = new Set(busyIds.value).add(row.id)
  try {
    await adminAnnouncementsApi.update(row.id, { status: nextStatus })
    toast.success(
      nextStatus === 'active'
        ? t('announcements.admin.published')
        : t('announcements.admin.archived')
    )
    await load()
    void announcementStore.fetchAnnouncements(true)
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    const next = new Set(busyIds.value)
    next.delete(row.id)
    busyIds.value = next
  }
}

async function confirmDelete() {
  if (!canWrite.value || !deleting.value || deleteLoading.value) return
  deleteLoading.value = true
  try {
    await adminAnnouncementsApi.remove(deleting.value.id)
    toast.success(t('announcements.admin.deleted'))
    deleting.value = null
    await load()
    void announcementStore.fetchAnnouncements(true)
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    deleteLoading.value = false
  }
}

watch(keyword, () => {
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reloadFromFirstPage, 250)
})
watch([statusFilter, typeFilter, pageSize], reloadFromFirstPage)
watch(page, () => void load())

onMounted(() => void load())
onBeforeUnmount(() => window.clearTimeout(searchTimer))
</script>

<template>
  <div data-announcements-page>
    <PageBreadcrumb
      :crumbs="[
        t('announcements.admin.breadcrumb.0'),
        t('announcements.admin.breadcrumb.1'),
      ]"
    />

    <ConsoleCard :padded="false">
      <div
        class="flex flex-wrap items-center gap-2.5 border-b border-[var(--border-subtle)] p-4"
        data-announcements-toolbar
      >
        <SearchInput
          v-model="keyword"
          :placeholder="t('announcements.admin.searchPlaceholder')"
          :aria-label="t('announcements.admin.searchPlaceholder')"
          name="announcement-search"
          size="compact"
          class="w-full sm:w-64"
        />
        <FilterSelect
          v-model="statusFilter"
          :options="statusOptions"
          :label="t('announcements.admin.statusFilter')"
          :prefix-label="t('announcements.admin.statusFilter')"
          variant="facet"
          size="compact"
        />
        <FilterSelect
          v-model="typeFilter"
          :options="typeOptions"
          :label="t('announcements.admin.typeFilter')"
          :prefix-label="t('announcements.admin.typeFilter')"
          variant="facet"
          size="compact"
        />
        <ConsoleButton
          v-if="hasFilters"
          variant="ghost"
          size="sm"
          @click="resetFilters"
        >
          {{ t('announcements.admin.resetFilters') }}
          <X :size="14" aria-hidden="true" />
        </ConsoleButton>

        <div
          class="ml-auto flex w-full items-center justify-end gap-2 sm:w-auto"
        >
          <IconButton
            :label="t('announcements.admin.refresh')"
            class="pencil-control rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)]"
            data-handdrawn="control"
            :disabled="loading"
            @click="load()"
          >
            <RefreshCw :size="15" :class="loading ? 'animate-spin' : ''" />
          </IconButton>
          <ConsoleButton
            v-if="canWrite"
            data-announcement-create
            @click="openCreate"
          >
            <Plus :size="15" :stroke-width="2.5" aria-hidden="true" />
            {{ t('announcements.admin.create') }}
          </ConsoleButton>
        </div>
      </div>

      <div class="hidden lg:block">
        <DataTable
          :columns="columns"
          :rows="rows"
          row-key="id"
          :loading="loading"
          :skeleton-rows="8"
          adaptive-scroll
          :page-size="pageSize"
          min-table-width="920px"
          :scroll-region-label="t('announcements.admin.title')"
          :empty-title="t('announcements.admin.emptyTitle')"
          :empty-hint="t('announcements.admin.emptyHint')"
        >
          <template #cell-title="{ row }">
            <div class="min-w-0 space-y-0.5">
              <div class="flex items-center gap-1.5">
                <Pin
                  v-if="(row as AdminAnnouncement).pinned"
                  :size="13"
                  class="shrink-0 text-[var(--accent-text)]"
                  :aria-label="t('announcements.pinned')"
                />
                <span
                  class="truncate text-sm font-semibold text-[var(--text-primary)]"
                  :title="(row as AdminAnnouncement).title"
                >
                  {{ (row as AdminAnnouncement).title }}
                </span>
              </div>
              <p class="truncate text-xs text-[var(--text-tertiary)]">
                #{{ (row as AdminAnnouncement).id }} ·
                {{
                  formatUnixDateTime(
                    (row as AdminAnnouncement).created_at,
                    locale
                  )
                }}
                ·
                {{
                  announcementExcerpt((row as AdminAnnouncement).content, 72)
                }}
              </p>
            </div>
          </template>

          <template #cell-type="{ row }">
            <StatusChip
              :tone="announcementTypeTone((row as AdminAnnouncement).type)"
            >
              {{ t(`announcements.types.${(row as AdminAnnouncement).type}`) }}
            </StatusChip>
          </template>

          <template #cell-status="{ row }">
            <div class="flex flex-wrap items-center gap-1">
              <StatusChip
                :tone="
                  announcementStatusTone((row as AdminAnnouncement).status)
                "
              >
                {{
                  t(
                    `announcements.statuses.${(row as AdminAnnouncement).status}`
                  )
                }}
              </StatusChip>
              <StatusChip
                v-if="announcementLiveState(row as AdminAnnouncement, nowSec)"
                tone="warning"
              >
                {{
                  t(
                    `announcements.liveStates.${announcementLiveState(row as AdminAnnouncement, nowSec)}`
                  )
                }}
              </StatusChip>
            </div>
          </template>

          <template #cell-notify_mode="{ row }">
            <StatusChip
              :tone="
                (row as AdminAnnouncement).notify_mode === 'popup'
                  ? 'accent'
                  : 'neutral'
              "
            >
              {{
                t(
                  `announcements.notifyModes.${(row as AdminAnnouncement).notify_mode}`
                )
              }}
            </StatusChip>
          </template>

          <template #cell-schedule="{ row }">
            <div
              class="space-y-0.5 text-xs tabular-nums text-[var(--text-secondary)]"
            >
              <p class="truncate">
                {{
                  (row as AdminAnnouncement).starts_at
                    ? formatUnixDateTime(
                        (row as AdminAnnouncement).starts_at!,
                        locale
                      )
                    : t('announcements.timeImmediate')
                }}
              </p>
              <p class="truncate text-[var(--text-tertiary)]">
                {{
                  (row as AdminAnnouncement).ends_at
                    ? formatUnixDateTime(
                        (row as AdminAnnouncement).ends_at!,
                        locale
                      )
                    : t('announcements.timeNever')
                }}
              </p>
            </div>
          </template>

          <template #cell-read_count="{ row }">
            <div class="flex justify-end">
              <button
                type="button"
                class="-mr-2 inline-flex min-h-8 items-center rounded-lg px-2 font-mono text-xs font-medium tabular-nums text-[var(--accent-text)] transition-colors hover:bg-[var(--accent-soft)] focus-ring"
                :data-announcement-read-count="(row as AdminAnnouncement).id"
                @click="readStatusTarget = row as AdminAnnouncement"
              >
                {{ (row as AdminAnnouncement).read_count }}
              </button>
            </div>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-1">
              <IconButton
                :label="t('announcements.admin.preview')"
                :data-announcement-preview="(row as AdminAnnouncement).id"
                @click="previewing = row as AdminAnnouncement"
              >
                <Eye :size="16" aria-hidden="true" />
              </IconButton>
              <IconButton
                :label="t('announcements.admin.readStatus')"
                :data-announcement-reads="(row as AdminAnnouncement).id"
                @click="readStatusTarget = row as AdminAnnouncement"
              >
                <BarChart3 :size="16" aria-hidden="true" />
              </IconButton>
              <template v-if="canWrite">
                <IconButton
                  :label="
                    (row as AdminAnnouncement).status === 'active'
                      ? t('announcements.admin.archive')
                      : t('announcements.admin.publish')
                  "
                  :disabled="busyIds.has((row as AdminAnnouncement).id)"
                  :data-announcement-toggle="(row as AdminAnnouncement).id"
                  @click="toggleStatus(row as AdminAnnouncement)"
                >
                  <Archive
                    v-if="(row as AdminAnnouncement).status === 'active'"
                    :size="16"
                    aria-hidden="true"
                  />
                  <Send v-else :size="16" aria-hidden="true" />
                </IconButton>
                <IconButton
                  :label="t('announcements.admin.edit')"
                  :data-announcement-edit="(row as AdminAnnouncement).id"
                  @click="openEdit(row as AdminAnnouncement)"
                >
                  <Pencil :size="16" aria-hidden="true" />
                </IconButton>
                <IconButton
                  tone="danger"
                  :label="t('announcements.admin.delete')"
                  :data-announcement-delete="(row as AdminAnnouncement).id"
                  @click="deleting = row as AdminAnnouncement"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </IconButton>
              </template>
            </div>
          </template>
        </DataTable>
      </div>

      <AnnouncementMobileList
        :rows="rows"
        :loading="loading"
        :can-write="canWrite"
        :busy-ids="busyIds"
        :now-sec="nowSec"
        @preview="previewing = $event"
        @read-status="readStatusTarget = $event"
        @toggle-status="toggleStatus"
        @edit="openEdit"
        @delete="deleting = $event"
      />

      <div
        v-if="total > 0"
        class="border-t border-[var(--border-subtle)] px-4 py-3"
      >
        <TablePagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :total="total"
        />
      </div>
    </ConsoleCard>

    <AnnouncementFormModal
      :open="formOpen"
      :editing="editing"
      :saving="formSaving"
      :error-message="formError"
      @close="closeForm"
      @submit="handleSave"
    />

    <AnnouncementDetailModal
      :open="previewing !== null"
      :announcement="previewing"
      preview
      @close="previewing = null"
    />

    <AnnouncementReadStatusModal
      :open="readStatusTarget !== null"
      :announcement="readStatusTarget"
      @close="readStatusTarget = null"
    />

    <ConfirmDialog
      :open="deleting !== null"
      :title="t('announcements.admin.deleteTitle')"
      :message="
        t('announcements.admin.deleteMessage', {
          title: deleting?.title ?? '',
        })
      "
      :confirm-text="t('announcements.admin.delete')"
      :loading="deleteLoading"
      @confirm="confirmDelete"
      @cancel="deleting = null"
    />
  </div>
</template>
