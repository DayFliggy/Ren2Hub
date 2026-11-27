<script setup lang="ts">
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Download, Eye } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, routeLocationKey, routerKey } from 'vue-router'

import { api } from '@/api/console'
import { LOG_EXPORT_STALLED } from '@/api/logExport'
import { parseLogPage } from '@/api/liveContracts'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import IconButton from '@/components/common/IconButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import LogBillingDetail from '@/components/console/log-ui/LogBillingDetail.vue'
import LogFilterToolbar, {
  type ActiveFilterChip,
} from '@/components/console/log-ui/LogFilterToolbar.vue'
import type { LogSearchField } from '@/components/console/log-ui/LogUnifiedSearch.vue'
import LogsNavTabs from '@/components/console/log-ui/LogsNavTabs.vue'
import {
  classifyQuotaLog,
  QUOTA_LOG_TYPES_PARAM,
  quotaLogDescription,
  type QuotaEntrySummary,
} from '@/components/console/log-ui/logClassification'
import {
  exportLogFiles,
  LOG_EXPORT_HEADERS,
  LOG_EXPORT_SESSION_CHANGED,
  type LogExportProgress,
  type LogExportType,
} from '@/components/console/log-ui/logExport'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { getLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { LogItem } from '@/types/console'
import { formatTime, localDayEndMs, localDayStartMs } from '@/utils/format'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const toast = useToast()
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)
const hasRouter = Boolean(router)

const canReadAll = computed(() => auth.isAdmin)
const canExport = computed(
  () => auth.isAdmin || app.status.log_export_enabled !== false
)

function queryStringParam(name: string): string {
  const value = route?.query?.[name]
  return typeof value === 'string' ? value : ''
}

const rows = ref<LogItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(true)
const loadError = ref<string | null>(null)

const requestId = ref(queryStringParam('request_id'))
const usernameFilter = ref('')
const type = ref('')
const scope = ref<'self' | 'all'>(
  queryStringParam('scope') === 'all' && canReadAll.value ? 'all' : 'self'
)
const readingAll = computed(() => scope.value === 'all' && canReadAll.value)
const startDate = ref('')
const endDate = ref('')

const detailLog = ref<LogItem | null>(null)
const exportOpen = ref(false)
const exportType = ref<LogExportType>('csv')
const exportStartDate = ref('')
const exportEndDate = ref('')

const scopeOptions = computed(() => [
  { value: 'self', label: t('quotaLogs.scopeSelf') },
  { value: 'all', label: t('quotaLogs.scopeAll') },
])

const typeOptions = computed(() => [
  { value: '', label: t('quotaLogs.typeAll') },
  {
    value: 'topup',
    label: t('quotaLogs.typeTopup'),
    tone: 'success' as const,
  },
  {
    value: 'refund',
    label: t('quotaLogs.typeRefund'),
    tone: 'warning' as const,
  },
  {
    value: 'system',
    label: t('quotaLogs.typeSystem'),
    tone: 'info' as const,
  },
])

const activeSearchField = ref('request_id')

const searchFields = computed<LogSearchField[]>(() => [
  {
    key: 'request_id',
    label: t('logs.searchFields.requestId'),
    placeholder: t('logs.keywordPlaceholder'),
    inputName: 'quota-log-search',
  },
  ...(readingAll.value
    ? [
        {
          key: 'username',
          label: t('logs.searchFields.username'),
          placeholder: t('logs.usernamePlaceholder'),
          inputName: 'quota-log-username',
        },
      ]
    : []),
])

const searchValues = computed<Record<string, string>>(() => ({
  request_id: requestId.value,
  username: usernameFilter.value,
}))

function updateSearchValue(key: string, value: string): void {
  if (key === 'request_id') requestId.value = value
  else if (key === 'username') usernameFilter.value = value
}

const secondaryActiveCount = computed(
  () =>
    Number(Boolean(type.value)) +
    Number(Boolean(startDate.value || endDate.value))
)

const extraActiveChips = computed<ActiveFilterChip[]>(() => {
  const chips: ActiveFilterChip[] = []
  if (type.value) {
    const label =
      typeOptions.value.find((opt) => opt.value === type.value)?.label ??
      type.value
    chips.push({ key: 'type', label: t('quotaLogs.typeLabel'), value: label })
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
  if (key === 'type') {
    type.value = ''
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
  requestId.value = ''
  usernameFilter.value = ''
  type.value = ''
  startDate.value = ''
  endDate.value = ''
}

const columns = computed<TableColumn[]>(() => [
  { key: 'created', label: t('quotaLogs.colTime'), width: '150px' },
  ...(readingAll.value
    ? [{ key: 'account', label: t('quotaLogs.colAccount'), width: '130px' }]
    : []),
  { key: 'kind', label: t('quotaLogs.colKind'), width: '132px' },
  { key: 'summary', label: t('quotaLogs.colSummary') },
  {
    key: 'amount',
    label: t('quotaLogs.colAmount'),
    width: '160px',
    align: 'right',
  },
  { key: 'reference', label: t('quotaLogs.colReference'), width: '188px' },
  {
    key: 'detail',
    label: t('quotaLogs.colDetail'),
    width: '72px',
    align: 'right',
  },
])

const amountToneClass: Record<QuotaEntrySummary['amountTone'], string> = {
  success: 'text-[var(--status-success-text)]',
  danger: 'text-[var(--status-danger-text)]',
  warning: 'text-[var(--status-warning-text)]',
  neutral: 'text-[var(--text-secondary)]',
}

function listEndpoint(): '/api/log/self' | '/api/log/' {
  return readingAll.value ? '/api/log/' : '/api/log/self'
}

function exportEndpoint(): '/api/log/self/export' | '/api/log/export' {
  return readingAll.value ? '/api/log/export' : '/api/log/self/export'
}

function currentParams(start = startDate.value, end = endDate.value) {
  const typeMap: Record<string, number> = {
    topup: 1,
    system: 4,
    refund: 6,
  }
  const singleType = type.value ? (typeMap[type.value] ?? 0) : 0
  const requestedStart = start ? Math.floor(localDayStartMs(start) / 1000) : 0
  const endTimestamp = end ? Math.floor(localDayEndMs(end) / 1000) : 0
  const username = readingAll.value ? usernameFilter.value.trim() : ''

  return {
    p: page.value,
    page_size: pageSize.value,
    request_id: requestId.value.trim(),
    type: singleType,
    ...(singleType === 0 ? { types: QUOTA_LOG_TYPES_PARAM } : {}),
    ...(username ? { username } : {}),
    start_timestamp: requestedStart,
    end_timestamp: endTimestamp,
  }
}

const listRequest = useLatestRequest()

async function load() {
  loading.value = true
  loadError.value = null
  const endpoint = listEndpoint()
  const result = await listRequest.run(async (signal) =>
    parseLogPage(
      await api.get<unknown>(endpoint, currentParams(), { signal }),
      endpoint
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    rows.value = []
    total.value = 0
    loadError.value =
      result.error instanceof ApiError
        ? result.error.message
        : t('common.failed')
    toast.error(loadError.value)
    return
  }
  rows.value = result.value.items
  total.value = result.value.total
}

let searchTimer = 0
function reload() {
  window.clearTimeout(searchTimer)
  if (page.value === 1) void load()
  else page.value = 1
}

watch([requestId, usernameFilter], () => {
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reload, 300)
})
watch([type, startDate, endDate], reload)
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
}
watch(pageSize, reload)
watch(page, load)

function openExport() {
  exportStartDate.value = startDate.value
  exportEndDate.value = endDate.value
  exportOpen.value = true
}

function closeExport() {
  exportController?.abort()
  exportOpen.value = false
}

const MULTI_FILE_NOTICE_MS = 10_000
const exporting = ref(false)
const exportProgress = ref<LogExportProgress>({ count: 0, files: 0 })
let exportController: AbortController | null = null

function download(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

function exportErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.code === LOG_EXPORT_STALLED)
    return t('logs.exportStalled')
  if (error instanceof ApiError && error.code === LOG_EXPORT_SESSION_CHANGED)
    return t('logs.exportSessionChanged')
  return error instanceof ApiError ? error.message : String(error)
}

function cancelExport() {
  exportController?.abort()
}

async function doExport() {
  exportController?.abort()
  const controller = new AbortController()
  exportController = controller
  exporting.value = true
  exportProgress.value = { count: 0, files: 0 }
  let savedFiles = 0
  try {
    const {
      p: _p,
      page_size: _pageSize,
      ...filters
    } = currentParams(exportStartDate.value, exportEndDate.value)
    const { count, files } = await exportLogFiles({
      filters,
      endpoint: exportEndpoint(),
      filenamePrefix: 'ren2hub-quota-logs',
      range: { start: exportStartDate.value, end: exportEndDate.value },
      type: exportType.value,
      headers: LOG_EXPORT_HEADERS.map((key) => t(`logs.exportHeaders.${key}`)),
      locale: getLocale(),
      signal: controller.signal,
      save: (blob, filename) => {
        savedFiles++
        download(blob, filename)
      },
      onProgress: (progress) => {
        if (exportController === controller) exportProgress.value = progress
      },
    })
    exportOpen.value = false
    if (files > 1)
      toast.push(
        'success',
        t('logs.exportedParts', { count, files }),
        MULTI_FILE_NOTICE_MS
      )
    else toast.success(t('logs.exported', { count }))
  } catch (error) {
    if (controller.signal.aborted) {
      if (savedFiles > 0)
        toast.warning(t('logs.exportCancelledPartial', { files: savedFiles }))
      return
    }
    const reason = exportErrorMessage(error)
    toast.error(
      savedFiles > 0
        ? t('logs.exportFailedPartial', { reason, files: savedFiles })
        : reason
    )
  } finally {
    if (exportController === controller) {
      exportController = null
      exporting.value = false
    }
  }
}

onMounted(load)
onBeforeUnmount(() => {
  exportController?.abort()
  window.clearTimeout(searchTimer)
})
</script>

<template>
  <div data-quota-log-page class="mx-auto max-w-[var(--console-list-width)]">
    <PageBreadcrumb
      :crumbs="[t('logs.breadcrumb.0'), t('quotaLogs.breadcrumb')]"
    >
      <template #action>
        <LogsNavTabs active="quota" />
      </template>
    </PageBreadcrumb>

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
          :label="t('quotaLogs.scopeLabel')"
          :prefix-label="t('quotaLogs.scopeLabel') + ':'"
          class="w-full sm:w-auto"
        />
        <FilterSelect
          v-model="type"
          :options="typeOptions"
          :label="t('quotaLogs.typeLabel')"
          :prefix-label="t('quotaLogs.typeLabel') + ':'"
          class="w-full sm:w-auto"
        />
        <DateRangePicker
          v-model:start="startDate"
          v-model:end="endDate"
          class="w-full sm:w-56"
        />
      </template>
      <template v-if="canExport" #actions>
        <ConsoleButton
          class="w-full sm:w-auto"
          variant="secondary"
          @click="openExport"
        >
          <Download :size="16" aria-hidden="true" />
          {{ t('common.export') }}
        </ConsoleButton>
      </template>
    </LogFilterToolbar>

    <ConsoleCard v-if="loadError && !loading" :padded="false">
      <EmptyState
        role="alert"
        :title="t('quotaLogs.loadFailed')"
        :hint="loadError"
        illustration="empty-logs"
      >
        <ConsoleButton class="mt-4" variant="secondary" @click="load">
          {{ t('common.retry') }}
        </ConsoleButton>
      </EmptyState>
    </ConsoleCard>

    <ConsoleCard v-else class="hidden lg:block" :padded="false">
      <DataTable
        :columns="columns"
        :rows="rows"
        row-key="id"
        :loading="loading"
        :skeleton-rows="pageSize"
        adaptive-scroll
        :page-size="pageSize"
        min-table-width="920px"
        :scroll-region-label="t('quotaLogs.breadcrumb')"
        :empty-title="t('quotaLogs.emptyTitle')"
        :empty-hint="t('quotaLogs.emptyHint')"
        empty-illustration="empty-logs"
      >
        <template #cell-created="{ row }">
          <span
            class="whitespace-nowrap font-mono text-xs tabular-nums text-[var(--text-tertiary)]"
          >
            {{ formatTime((row as LogItem).created) }}
          </span>
        </template>
        <template #cell-account="{ row }">
          <span
            class="block truncate text-sm font-semibold text-[var(--text-primary)]"
            :title="(row as LogItem).username || '-'"
          >
            {{ (row as LogItem).username || '-' }}
          </span>
        </template>
        <template #cell-kind="{ row }">
          <StatusChip :tone="classifyQuotaLog(row as LogItem).tone">
            {{ t(classifyQuotaLog(row as LogItem).kindLabelKey) }}
          </StatusChip>
        </template>
        <template #cell-summary="{ row }">
          <p
            data-quota-log-summary
            class="line-clamp-2 text-sm leading-5 text-[var(--text-secondary)]"
            :title="quotaLogDescription(row as LogItem, getLocale(), t)"
          >
            {{ quotaLogDescription(row as LogItem, getLocale(), t) || '—' }}
          </p>
        </template>
        <template #cell-amount="{ row }">
          <span
            data-quota-log-amount
            class="whitespace-nowrap font-mono text-sm font-semibold tabular-nums"
            :class="
              amountToneClass[classifyQuotaLog(row as LogItem).amountTone]
            "
          >
            {{ classifyQuotaLog(row as LogItem).amountText ?? '—' }}
          </span>
        </template>
        <template #cell-reference="{ row }">
          <div class="min-w-0">
            <RouterLink
              v-if="hasRouter && classifyQuotaLog(row as LogItem).linkedTask"
              :to="{
                name: classifyQuotaLog(row as LogItem).linkedTask!.routeName,
                query: classifyQuotaLog(row as LogItem).linkedTask!.query,
              }"
              class="block truncate font-mono text-xs text-[var(--accent-text)] underline-offset-2 hover:underline"
              :title="
                t('quotaLogs.openTask', {
                  id: classifyQuotaLog(row as LogItem).linkedTask!.taskId,
                })
              "
            >
              {{ classifyQuotaLog(row as LogItem).linkedTask!.taskId }}
            </RouterLink>
            <span
              v-else-if="classifyQuotaLog(row as LogItem).linkedTask"
              class="block truncate font-mono text-xs text-[var(--text-secondary)]"
            >
              {{ classifyQuotaLog(row as LogItem).linkedTask!.taskId }}
            </span>
            <p
              v-if="(row as LogItem).model"
              class="truncate font-mono text-xs text-[var(--text-tertiary)]"
              :title="(row as LogItem).model"
            >
              {{ (row as LogItem).model }}
            </p>
            <p
              v-else-if="(row as LogItem).request_id"
              class="truncate font-mono text-xs text-[var(--text-tertiary)]"
              :title="(row as LogItem).request_id"
            >
              {{ (row as LogItem).request_id }}
            </p>
            <span
              v-else-if="!classifyQuotaLog(row as LogItem).linkedTask"
              class="text-xs text-[var(--text-tertiary)]"
            >
              —
            </span>
          </div>
        </template>
        <template #cell-detail="{ row }">
          <div class="flex justify-end">
            <IconButton
              :label="t('quotaLogs.viewDetails')"
              @click="detailLog = row as LogItem"
            >
              <Eye :size="16" aria-hidden="true" />
            </IconButton>
          </div>
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

    <section
      v-if="!loadError || loading"
      class="lg:hidden"
      :aria-label="t('quotaLogs.breadcrumb')"
    >
      <div v-if="loading" class="sibling-space-y-3" aria-busy="true">
        <SkeletonBlock
          v-for="index in Math.min(pageSize, 5)"
          :key="index"
          bordered
          rounded="xl"
          class="h-40"
        />
      </div>

      <div v-else-if="rows.length" class="sibling-space-y-3">
        <article
          v-for="row in rows"
          :key="row.id"
          data-quota-log-card
          class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4"
          :aria-label="
            t('quotaLogs.mobileCardLabel', {
              kind: t(classifyQuotaLog(row).kindLabelKey),
            })
          "
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-1.5">
                <StatusChip :tone="classifyQuotaLog(row).tone">
                  {{ t(classifyQuotaLog(row).kindLabelKey) }}
                </StatusChip>
                <span
                  v-if="readingAll && row.username"
                  class="text-xs font-semibold text-[var(--text-primary)]"
                >
                  {{ row.username }}
                </span>
              </div>
              <p
                class="mt-1 font-mono text-xs tabular-nums text-[var(--text-tertiary)]"
              >
                {{ formatTime(row.created) }}
              </p>
            </div>
            <span
              data-quota-log-amount
              class="shrink-0 font-mono text-sm font-semibold tabular-nums"
              :class="amountToneClass[classifyQuotaLog(row).amountTone]"
            >
              {{ classifyQuotaLog(row).amountText ?? '—' }}
            </span>
          </div>

          <p
            data-quota-log-summary
            class="mt-3 text-sm leading-5 text-[var(--text-secondary)]"
          >
            {{ quotaLogDescription(row, getLocale(), t) || '—' }}
          </p>

          <div
            class="mt-3 flex items-center justify-between gap-2 border-t border-[var(--border-subtle)] pt-3"
          >
            <div class="min-w-0 truncate text-xs text-[var(--text-tertiary)]">
              <RouterLink
                v-if="hasRouter && classifyQuotaLog(row).linkedTask"
                :to="{
                  name: classifyQuotaLog(row).linkedTask!.routeName,
                  query: classifyQuotaLog(row).linkedTask!.query,
                }"
                class="font-mono text-[var(--accent-text)] underline-offset-2 hover:underline"
              >
                {{ classifyQuotaLog(row).linkedTask!.taskId }}
              </RouterLink>
              <span v-else class="font-mono">
                {{ row.model || row.request_id || '—' }}
              </span>
            </div>
            <ConsoleButton size="sm" variant="ghost" @click="detailLog = row">
              <Eye :size="15" aria-hidden="true" />
              {{ t('quotaLogs.viewDetails') }}
            </ConsoleButton>
          </div>
        </article>
      </div>

      <div
        v-else
        class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <EmptyState
          :title="t('quotaLogs.emptyTitle')"
          :hint="t('quotaLogs.emptyHint')"
          illustration="empty-logs"
        />
      </div>

      <div
        class="mt-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <TablePagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :total="total"
        />
      </div>
    </section>

    <ConsoleModal
      :open="detailLog !== null"
      :title="t('quotaLogs.detailTitle')"
      size="md"
      @close="detailLog = null"
    >
      <template v-if="detailLog">
        <LogBillingDetail :log="detailLog" mode="quota" />
      </template>
      <template #footer>
        <div class="flex justify-end">
          <ConsoleButton variant="secondary" @click="detailLog = null">
            {{ t('common.close') }}
          </ConsoleButton>
        </div>
      </template>
    </ConsoleModal>

    <ConsoleModal
      :open="exportOpen"
      :title="t('logs.exportTitle')"
      size="lg"
      @close="closeExport"
    >
      <FormField group :label="t('logs.dateRange')">
        <DateRangePicker
          v-model:start="exportStartDate"
          v-model:end="exportEndDate"
          presentation="inline"
          class="w-full"
        />
        <p class="mt-2 text-sm text-[var(--text-secondary)]">
          {{ t('logs.exportRangeHint') }}
        </p>
      </FormField>
      <FormField group :label="t('logs.docType')">
        <FilterSelect
          v-model="exportType"
          :options="[
            { value: 'csv', label: 'CSV' },
            { value: 'excel', label: 'Excel' },
            { value: 'json', label: 'JSON' },
          ]"
          :label="t('logs.docType')"
        />
      </FormField>
      <template #footer>
        <p
          v-if="exporting && exportProgress.files > 0"
          class="mb-2 text-sm text-[var(--text-secondary)]"
        >
          {{ t('logs.exportMultipleFiles') }}
        </p>
        <p
          v-if="exporting"
          class="mb-3 text-sm text-[var(--text-secondary)]"
          role="status"
          aria-live="polite"
        >
          {{
            exportProgress.files > 0
              ? t('logs.exportProgressFiles', exportProgress)
              : t('logs.exportProgress', exportProgress)
          }}
        </p>
        <ConsoleButton
          v-if="exporting"
          size="lg"
          block
          variant="secondary"
          @click="cancelExport"
        >
          {{ t('logs.cancelExport') }}
        </ConsoleButton>
        <ConsoleButton v-else size="lg" block @click="doExport">
          {{ t('common.confirm') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>
  </div>
</template>
