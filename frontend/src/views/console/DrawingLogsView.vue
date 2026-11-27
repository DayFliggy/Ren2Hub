<script setup lang="ts">
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { routeLocationKey, routerKey } from 'vue-router'

import { api } from '@/api/console'
import { parseDrawingLogPage } from '@/api/liveContracts'
import { ApiError } from '@/api/types'
import type { DrawingLogItem } from '@/types/console'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import AsyncTaskSubTabs from '@/components/console/log-ui/AsyncTaskSubTabs.vue'
import LogFilterToolbar, {
  type ActiveFilterChip,
} from '@/components/console/log-ui/LogFilterToolbar.vue'
import type { LogSearchField } from '@/components/console/log-ui/LogUnifiedSearch.vue'
import LogsNavTabs from '@/components/console/log-ui/LogsNavTabs.vue'
import {
  relayTaskDurationSeconds,
  relayTaskStatusMeta,
} from '@/components/console/log-ui/relayTaskStatus'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { getLocale } from '@/i18n'
import { translateServerText } from '@/i18n/serverMessages'
import { useAuthStore } from '@/stores/auth'
import {
  formatQuota,
  formatTime,
  localDayEndMs,
  localDayStartMs,
} from '@/utils/format'
import { safeExternalUrl } from '@/utils/safeUrl'

const { t } = useI18n()
const toast = useToast()
const auth = useAuthStore()
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)

const canReadAll = computed(() => auth.isAdmin)

function queryStringParam(name: string): string {
  const value = route?.query?.[name]
  return typeof value === 'string' ? value : ''
}

const rows = ref<DrawingLogItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(true)
const loadError = ref<string | null>(null)

const taskId = ref(queryStringParam('mj_id'))
const status = ref('')
const channelId = ref('')
const scope = ref<'self' | 'all'>(
  queryStringParam('scope') === 'all' && canReadAll.value ? 'all' : 'self'
)
const readingAll = computed(() => scope.value === 'all' && canReadAll.value)
const startDate = ref('')
const endDate = ref('')

const detailRow = ref<DrawingLogItem | null>(null)

const scopeOptions = computed(() => [
  { value: 'self', label: t('relayLogs.scopeSelf') },
  { value: 'all', label: t('relayLogs.scopeAll') },
])

const statusOptions = computed(() => [
  { value: '', label: t('relayLogs.statusAll') },
  {
    value: 'SUCCESS',
    label: t('relayLogs.statusSuccess'),
    tone: 'success' as const,
  },
  {
    value: 'IN_PROGRESS',
    label: t('relayLogs.statusInProgress'),
    tone: 'info' as const,
  },
  {
    value: 'QUEUED',
    label: t('relayLogs.statusQueued'),
    tone: 'warning' as const,
  },
  {
    value: 'MODAL',
    label: t('relayLogs.statusWaiting'),
    tone: 'warning' as const,
  },
  {
    value: 'NOT_START',
    label: t('relayLogs.statusNotStart'),
  },
  {
    value: 'FAILURE',
    label: t('relayLogs.statusFailed'),
    tone: 'danger' as const,
  },
])

const activeSearchField = ref('mj_id')

const searchFields = computed<LogSearchField[]>(() => [
  {
    key: 'mj_id',
    label: t('logs.searchFields.taskId'),
    placeholder: t('relayLogs.taskIdPlaceholder'),
    inputName: 'drawing-task-search',
  },
  ...(readingAll.value
    ? [
        {
          key: 'channel_id',
          label: t('logs.searchFields.channelId'),
          placeholder: t('relayLogs.channelIdPlaceholder'),
          inputName: 'drawing-log-channel',
        },
      ]
    : []),
])

const searchValues = computed<Record<string, string>>(() => ({
  mj_id: taskId.value,
  channel_id: channelId.value,
}))

function updateSearchValue(key: string, value: string): void {
  if (key === 'mj_id') taskId.value = value
  else if (key === 'channel_id') channelId.value = value
}

const secondaryActiveCount = computed(
  () =>
    Number(Boolean(status.value)) +
    Number(Boolean(startDate.value || endDate.value))
)

const extraActiveChips = computed<ActiveFilterChip[]>(() => {
  const chips: ActiveFilterChip[] = []
  if (status.value) {
    const label =
      statusOptions.value.find((opt) => opt.value === status.value)?.label ??
      status.value
    chips.push({
      key: 'status',
      label: t('relayLogs.statusLabel'),
      value: label,
    })
  }
  if (startDate.value || endDate.value) {
    chips.push({
      key: 'date',
      label: t('logs.dateRange'),
      value: `${startDate.value || '…'} ~ ${endDate.value || '…'}`,
    })
  }
  return chips
})

const hasActiveFilters = computed(() => {
  const hasSearch = searchFields.value.some((f) =>
    Boolean(searchValues.value[f.key]?.trim())
  )
  return hasSearch || secondaryActiveCount.value > 0
})

function clearFilterChip(key: string): void {
  if (key === 'status') {
    status.value = ''
    return
  }
  if (key === 'date') {
    startDate.value = ''
    endDate.value = ''
    return
  }
  updateSearchValue(key, '')
}

function resetFilters(): void {
  taskId.value = ''
  status.value = ''
  channelId.value = ''
  startDate.value = ''
  endDate.value = ''
}

const columns = computed<TableColumn[]>(() => [
  { key: 'submitted', label: t('relayLogs.colSubmitted'), width: '170px' },
  ...(readingAll.value
    ? [{ key: 'account', label: t('relayLogs.colAccount'), width: '140px' }]
    : []),
  { key: 'task', label: t('relayLogs.colTask'), width: '190px' },
  { key: 'prompt', label: t('relayLogs.colPrompt') },
  { key: 'progress', label: t('relayLogs.colProgress'), width: '130px' },
  { key: 'result', label: t('relayLogs.colResult'), width: '110px' },
  {
    key: 'cost',
    label: t('relayLogs.colCost'),
    width: '100px',
    align: 'right',
  },
])

function endpoint(): '/api/mj/self' | '/api/mj/' {
  return readingAll.value ? '/api/mj/' : '/api/mj/self'
}

function currentParams() {
  // /api/mj filters compare against millisecond submit_time values and treat
  // any sent value, including 0, as a bound; an unset date is omitted.
  const startTimestamp = startDate.value
    ? localDayStartMs(startDate.value)
    : undefined
  const endTimestamp = endDate.value ? localDayEndMs(endDate.value) : undefined
  const statusVal = status.value.trim()
  const channelVal = readingAll.value ? channelId.value.trim() : ''
  return {
    p: page.value,
    page_size: pageSize.value,
    mj_id: taskId.value.trim(),
    ...(statusVal ? { status: statusVal } : {}),
    ...(channelVal ? { channel_id: channelVal } : {}),
    start_timestamp: startTimestamp,
    end_timestamp: endTimestamp,
  }
}

const listRequest = useLatestRequest()

function clearResults() {
  loading.value = true
  loadError.value = null
  rows.value = []
  total.value = 0
}

async function load() {
  window.clearTimeout(searchTimer)
  clearResults()
  const target = endpoint()
  const result = await listRequest.run(async (signal) =>
    parseDrawingLogPage(
      await api.get<unknown>(target, currentParams(), { signal }),
      target
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    loadError.value =
      result.error instanceof ApiError
        ? result.error.message
        : t('common.failed')
    toast.error(loadError.value)
    return
  }
  const pageResult = result.value
  rows.value = pageResult.items
  total.value = pageResult.total
}

let searchTimer = 0
function reload() {
  window.clearTimeout(searchTimer)
  if (page.value === 1) load()
  else page.value = 1
}
watch([taskId, channelId], () => {
  // The input already describes a new query during the debounce interval.
  listRequest.cancel()
  clearResults()
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reload, 300)
})
watch([status, startDate, endDate], reload)
watch(scope, (value) => {
  if (router && route) {
    void router.replace({
      query: { ...route.query, scope: value === 'all' ? 'all' : undefined },
    })
  }
  reload()
})
if (route) {
  watch(
    () => route.query.scope,
    (value) => {
      scope.value = value === 'all' && canReadAll.value ? 'all' : 'self'
    }
  )
  watch(
    () => route.query.mj_id,
    (value) => {
      const next = typeof value === 'string' ? value : ''
      if (next !== taskId.value) taskId.value = next
    }
  )
}
watch(pageSize, reload)
watch(page, load)
onMounted(load)
onBeforeUnmount(() => window.clearTimeout(searchTimer))

function formatDuration(row: DrawingLogItem): string {
  const seconds = relayTaskDurationSeconds(
    row.submit_time,
    row.finish_time,
    'milliseconds'
  )
  return seconds === null ? '-' : `${seconds.toFixed(1)}s`
}

function resultUrl(row: DrawingLogItem): string | null {
  return safeExternalUrl(row.image_url || row.video_url)
}

function rawResultUrl(row: DrawingLogItem): string {
  return row.image_url || row.video_url
}

async function copyResultUrl(row: DrawingLogItem): Promise<void> {
  try {
    await navigator.clipboard.writeText(rawResultUrl(row))
    toast.success(t('common.copied'))
  } catch {
    toast.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <div class="mx-auto max-w-[var(--console-list-width)]">
    <PageBreadcrumb
      :crumbs="[t('logs.breadcrumb.0'), t('relayLogs.drawingBreadcrumb')]"
    >
      <template #action>
        <LogsNavTabs active="tasks" />
      </template>
    </PageBreadcrumb>

    <AsyncTaskSubTabs active="midjourney" :scope-all="readingAll" />

    <!-- filter toolbar -->
    <LogFilterToolbar
      :search-fields="searchFields"
      :search-values="searchValues"
      :search-field="activeSearchField"
      :secondary-active-count="secondaryActiveCount"
      :active-chips="extraActiveChips"
      :has-active-filters="hasActiveFilters"
      @update:search-value="updateSearchValue"
      @update:search-field="activeSearchField = $event"
      @clear-chip="clearFilterChip"
      @reset="resetFilters"
    >
      <template #filters>
        <FilterSelect
          v-if="canReadAll"
          v-model="scope"
          :options="scopeOptions"
          :label="t('relayLogs.scopeLabel')"
          :prefix-label="t('relayLogs.scopeLabel') + ':'"
          class="w-full sm:w-auto"
        />
        <FilterSelect
          v-model="status"
          :options="statusOptions"
          :label="t('relayLogs.statusLabel')"
          :prefix-label="t('relayLogs.statusLabel') + ':'"
          class="w-full sm:w-auto"
        />
        <DateRangePicker
          v-model:start="startDate"
          v-model:end="endDate"
          class="w-full sm:w-56"
        />
      </template>
    </LogFilterToolbar>

    <ConsoleCard class="hidden lg:block" :padded="false">
      <EmptyState
        v-if="loadError"
        role="alert"
        :title="t('logs.loadFailed')"
        :hint="loadError"
        illustration="empty-logs"
      >
        <ConsoleButton class="mt-4" variant="secondary" @click="load">
          {{ t('common.retry') }}
        </ConsoleButton>
      </EmptyState>
      <DataTable
        v-else
        :columns="columns"
        :rows="rows"
        row-key="id"
        :loading="loading"
        :skeleton-rows="pageSize"
        adaptive-scroll
        :page-size="pageSize"
        min-table-width="920px"
        :scroll-region-label="t('relayLogs.drawingBreadcrumb')"
        :empty-title="t('relayLogs.emptyTitle')"
        :empty-hint="t('relayLogs.emptyHint')"
        empty-illustration="empty-logs"
      >
        <template #cell-submitted="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-1.5">
            <span
              class="whitespace-nowrap font-mono text-xs tabular-nums text-[var(--text-tertiary)]"
            >
              {{
                formatTime(
                  Math.floor((row as DrawingLogItem).submit_time / 1000)
                )
              }}
            </span>
            <StatusChip
              :tone="relayTaskStatusMeta((row as DrawingLogItem).status).tone"
            >
              {{
                t(relayTaskStatusMeta((row as DrawingLogItem).status).labelKey)
              }}
            </StatusChip>
          </div>
        </template>
        <template #cell-account="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-0.5">
            <span class="text-xs font-semibold text-[var(--text-primary)]">
              {{
                (row as DrawingLogItem).user_id
                  ? t('relayLogs.userIdBadge', {
                      id: (row as DrawingLogItem).user_id,
                    })
                  : '-'
              }}
            </span>
            <span
              v-if="(row as DrawingLogItem).channel_id"
              class="font-mono text-[0.6875rem] text-[var(--text-tertiary)]"
            >
              {{
                t('relayLogs.channelIdBadge', {
                  id: (row as DrawingLogItem).channel_id,
                })
              }}
            </span>
          </div>
        </template>
        <template #cell-task="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-1">
            <span
              class="text-xs font-semibold text-[var(--text-primary)]"
              :title="(row as DrawingLogItem).action"
            >
              {{ (row as DrawingLogItem).action || '-' }}
            </span>
            <span
              class="block max-w-full truncate font-mono text-xs text-[var(--text-tertiary)]"
              :title="(row as DrawingLogItem).mj_id"
            >
              {{ (row as DrawingLogItem).mj_id || '-' }}
            </span>
          </div>
        </template>
        <template #cell-prompt="{ row }">
          <button
            v-if="
              (row as DrawingLogItem).prompt ||
              (row as DrawingLogItem).fail_reason
            "
            type="button"
            class="flex w-full min-w-0 max-w-full flex-col items-start gap-0.5 text-left text-xs underline-offset-2 hover:underline focus-ring"
            :title="t('relayLogs.viewDetails')"
            @click="detailRow = row as DrawingLogItem"
          >
            <span
              v-if="(row as DrawingLogItem).prompt"
              class="block max-w-full truncate text-[var(--text-secondary)]"
            >
              {{ (row as DrawingLogItem).prompt }}
            </span>
            <span
              v-if="(row as DrawingLogItem).fail_reason"
              class="block max-w-full truncate text-[var(--status-danger-text)]"
            >
              {{
                translateServerText(
                  getLocale(),
                  (row as DrawingLogItem).fail_reason
                )
              }}
            </span>
          </button>
          <span v-else class="text-xs text-[var(--text-tertiary)]">-</span>
        </template>
        <template #cell-progress="{ row }">
          <div
            class="flex min-w-0 flex-col items-start gap-1 font-mono tabular-nums"
          >
            <span class="text-xs text-[var(--text-secondary)]">
              {{ (row as DrawingLogItem).progress || '-' }}
            </span>
            <span class="text-xs text-[var(--text-tertiary)]">
              {{ formatDuration(row as DrawingLogItem) }}
            </span>
          </div>
        </template>
        <template #cell-result="{ row }">
          <a
            v-if="resultUrl(row as DrawingLogItem)"
            :href="resultUrl(row as DrawingLogItem) ?? undefined"
            target="_blank"
            rel="noopener noreferrer"
            class="text-xs text-[var(--accent-text)] underline-offset-2 hover:underline"
          >
            {{ t('relayLogs.viewResult') }}
          </a>
          <button
            v-else-if="rawResultUrl(row as DrawingLogItem)"
            type="button"
            class="text-xs text-[var(--text-secondary)] underline-offset-2 hover:underline"
            @click="copyResultUrl(row as DrawingLogItem)"
          >
            {{ t('relayLogs.copyResultLink') }}
          </button>
          <button
            v-else-if="(row as DrawingLogItem).fail_reason"
            type="button"
            class="text-xs text-[var(--status-danger-text)] underline-offset-2 hover:underline"
            @click="detailRow = row as DrawingLogItem"
          >
            {{ t('relayLogs.viewDetails') }}
          </button>
          <span v-else class="text-xs text-[var(--text-tertiary)]">-</span>
        </template>
        <template #cell-cost="{ row }">
          <span
            class="whitespace-nowrap font-mono text-sm font-semibold tabular-nums text-[var(--text-primary)]"
          >
            {{ formatQuota((row as DrawingLogItem).quota) }}
          </span>
        </template>

        <template #footer>
          <div class="border-t border-[var(--border-subtle)]">
            <TablePagination
              v-model:page="page"
              v-model:page-size="pageSize"
              :total="total"
            />
          </div>
        </template>
      </DataTable>
    </ConsoleCard>

    <!-- mobile cards -->
    <section class="lg:hidden" :aria-label="t('relayLogs.drawingBreadcrumb')">
      <div v-if="loading" class="sibling-space-y-3" aria-busy="true">
        <SkeletonBlock
          v-for="index in Math.min(pageSize, 5)"
          :key="index"
          bordered
          rounded="xl"
          class="h-36"
        />
      </div>

      <EmptyState
        v-else-if="loadError"
        role="alert"
        class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
        :title="t('logs.loadFailed')"
        :hint="loadError"
        illustration="empty-logs"
      >
        <ConsoleButton class="mt-4" variant="secondary" @click="load">
          {{ t('common.retry') }}
        </ConsoleButton>
      </EmptyState>

      <div v-else-if="rows.length" class="sibling-space-y-3">
        <article
          v-for="row in rows"
          :key="row.id"
          class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4"
        >
          <div class="flex items-center justify-between gap-2">
            <StatusChip :tone="relayTaskStatusMeta(row.status).tone">
              {{ t(relayTaskStatusMeta(row.status).labelKey) }}
            </StatusChip>
            <span
              class="font-mono text-xs tabular-nums text-[var(--text-tertiary)]"
            >
              {{ formatTime(Math.floor(row.submit_time / 1000)) }}
            </span>
          </div>
          <p
            v-if="readingAll && (row.user_id || row.channel_id)"
            class="mt-1.5 truncate text-xs text-[var(--text-secondary)]"
          >
            {{
              row.user_id ? t('relayLogs.userIdBadge', { id: row.user_id }) : ''
            }}
            <template v-if="row.channel_id">
              · {{ t('relayLogs.channelIdBadge', { id: row.channel_id }) }}
            </template>
          </p>
          <p
            class="mt-2 truncate font-mono text-xs text-[var(--text-tertiary)]"
          >
            {{ row.action || '-' }} · {{ row.mj_id || '-' }}
          </p>
          <button
            v-if="row.prompt || row.fail_reason"
            type="button"
            class="mt-2 flex min-h-9 w-full flex-col justify-center gap-0.5 text-left"
            @click="detailRow = row"
          >
            <span
              v-if="row.prompt"
              class="block w-full truncate text-sm text-[var(--text-secondary)]"
            >
              {{ row.prompt }}
            </span>
            <span
              v-if="row.fail_reason"
              class="block w-full truncate text-xs text-[var(--status-danger-text)]"
            >
              {{ translateServerText(getLocale(), row.fail_reason) }}
            </span>
          </button>
          <div class="mt-3 flex items-center justify-between gap-2">
            <span
              class="font-mono text-xs tabular-nums text-[var(--text-tertiary)]"
            >
              {{ row.progress || '-' }} · {{ formatDuration(row) }}
            </span>
            <span class="font-mono text-sm font-semibold tabular-nums">
              {{ formatQuota(row.quota) }}
            </span>
          </div>
          <div
            v-if="rawResultUrl(row)"
            class="mt-2 border-t border-[var(--border-subtle)] pt-2"
          >
            <a
              v-if="resultUrl(row)"
              :href="resultUrl(row) ?? undefined"
              target="_blank"
              rel="noopener noreferrer"
              class="inline-flex min-h-9 items-center text-xs font-medium text-[var(--accent-text)] underline-offset-2 hover:underline"
            >
              {{ t('relayLogs.viewResult') }}
            </a>
            <button
              v-else
              type="button"
              class="inline-flex min-h-9 items-center text-xs font-medium text-[var(--text-secondary)] underline-offset-2 hover:underline"
              @click="copyResultUrl(row)"
            >
              {{ t('relayLogs.copyResultLink') }}
            </button>
          </div>
        </article>
      </div>

      <div
        v-else
        class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <EmptyState
          :title="t('relayLogs.emptyTitle')"
          :hint="t('relayLogs.emptyHint')"
          illustration="empty-logs"
        />
      </div>

      <div
        v-if="!loadError"
        class="mt-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <TablePagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :total="total"
        />
      </div>
    </section>

    <!-- prompt / failure details -->
    <ConsoleModal
      :open="detailRow !== null"
      :title="t('relayLogs.detailTitle')"
      size="md"
      @close="detailRow = null"
    >
      <div v-if="detailRow" class="sibling-space-y-4">
        <div
          class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3.5"
        >
          <p class="mb-1 text-xs font-semibold text-[var(--text-tertiary)]">
            {{ t('relayLogs.colPrompt') }}
          </p>
          <p
            class="whitespace-pre-wrap break-words text-sm leading-6 text-[var(--text-primary)]"
          >
            {{ detailRow.prompt || '-' }}
          </p>
        </div>
        <div
          v-if="detailRow.prompt_en && detailRow.prompt_en !== detailRow.prompt"
          class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3.5"
        >
          <p class="mb-1 text-xs font-semibold text-[var(--text-tertiary)]">
            {{ t('relayLogs.promptEn') }}
          </p>
          <p
            class="whitespace-pre-wrap break-words text-sm leading-6 text-[var(--text-secondary)]"
          >
            {{ detailRow.prompt_en }}
          </p>
        </div>
        <div
          v-if="detailRow.fail_reason"
          class="rounded-lg border border-[var(--status-danger)] bg-[var(--status-danger-soft)] p-3.5"
        >
          <p
            class="mb-1 text-xs font-semibold text-[var(--status-danger-text)]"
          >
            {{ t('relayLogs.failReason') }}
          </p>
          <p
            class="whitespace-pre-wrap break-words text-sm leading-6 text-[var(--status-danger-text)]"
          >
            {{ translateServerText(getLocale(), detailRow.fail_reason) }}
          </p>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end">
          <ConsoleButton variant="secondary" @click="detailRow = null">
            {{ t('common.close') }}
          </ConsoleButton>
        </div>
      </template>
    </ConsoleModal>
  </div>
</template>
