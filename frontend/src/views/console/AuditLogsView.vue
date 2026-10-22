<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Download, Eye } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { api } from '@/api/console'
import { parseAuditLogPage } from '@/api/liveContracts'
import { ApiError, type PageResult } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import LogsNavTabs from '@/components/console/log-ui/LogsNavTabs.vue'
import {
  auditLogAuthMethodKey,
  auditLogCategoryLabel,
  auditLogCategoryTone,
  auditLogParamLabel,
  auditLogParamValue,
  auditLogRequestText,
  auditLogResultKey,
  auditLogResultTone,
  auditLogRoleKey,
  auditLogSummary,
} from '@/components/console/log-ui/auditLog'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import type { AuditLogCategory, AuditLogItem } from '@/types/console'
import {
  dateInputValue,
  formatTime,
  localDayEndMs,
  localDayStartMs,
} from '@/utils/format'
import { serializeSpreadsheet } from '@/utils/spreadsheetExport'

const { t } = useI18n()
const toast = useToast()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

/** Reading other accounts needs the audit permission; members see only their own events. */
const canReadAll = computed(
  () => auth.isAdmin && auth.hasPermission('audit', 'read')
)

const rows = ref<AuditLogItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(true)
const loadError = ref(false)

const requestId = ref('')
const category = ref<'all' | AuditLogCategory>('all')
const outcome = ref<'all' | 'true' | 'false'>('all')
const tokenRef = ref('')
const scope = ref<'self' | 'all'>(
  route.query.scope === 'all' && canReadAll.value ? 'all' : 'self'
)
const startDate = ref(dateInputValue(-29))
const endDate = ref(dateInputValue())

const detailLog = ref<AuditLogItem | null>(null)
const exportOpen = ref(false)
const exportType = ref('csv')
const exporting = ref(false)

const columns = computed<TableColumn[]>(() => [
  { key: 'created', label: t('auditLogs.columns.time'), width: '150px' },
  { key: 'actor', label: t('auditLogs.columns.actor'), width: '170px' },
  {
    key: 'result',
    label: t('auditLogs.columns.categoryResult'),
    width: '150px',
  },
  { key: 'summary', label: t('auditLogs.columns.summary') },
  { key: 'source', label: t('auditLogs.columns.source'), width: '150px' },
  { key: 'request', label: t('auditLogs.columns.request'), width: '220px' },
  {
    key: 'detail',
    label: t('auditLogs.columns.detail'),
    width: '72px',
    align: 'right',
  },
])

const categoryOptions = computed(() => [
  { value: 'all', label: t('auditLogs.category.all') },
  {
    value: 'operation',
    label: t('auditLogs.category.operation'),
    tone: 'accent' as const,
  },
  {
    value: 'security',
    label: t('auditLogs.category.security'),
    tone: 'warning' as const,
  },
  {
    value: 'login',
    label: t('auditLogs.category.login'),
    tone: 'info' as const,
  },
  {
    value: 'access_token',
    label: t('auditLogs.category.accessToken'),
    tone: 'warning' as const,
  },
])

const scopeOptions = computed(() => [
  { value: 'self', label: t('auditLogs.scope.self') },
  { value: 'all', label: t('auditLogs.scope.all') },
])

const outcomeOptions = computed(() => [
  { value: 'all', label: t('auditLogs.result.all') },
  {
    value: 'true',
    label: t('auditLogs.result.success'),
    tone: 'success' as const,
  },
  {
    value: 'false',
    label: t('auditLogs.result.failed'),
    tone: 'danger' as const,
  },
])

const detailParams = computed(() =>
  Object.entries(detailLog.value?.params ?? {}).filter(
    ([, value]) => value !== undefined
  )
)

const readingAll = computed(() => scope.value === 'all' && canReadAll.value)

function endpoint(): string {
  return readingAll.value ? '/api/audit' : '/api/audit/self'
}

function currentParams() {
  return {
    p: page.value,
    page_size: pageSize.value,
    category: category.value === 'all' ? '' : category.value,
    success: outcome.value === 'all' ? '' : outcome.value,
    // The token filter is only shown on the all-accounts view; a value left
    // over from it must not silently narrow the personal view.
    token_ref: readingAll.value ? tokenRef.value.trim() : '',
    // The audit API matches request_id exactly (varchar 64); it has no keyword.
    request_id: requestId.value.trim(),
    start_timestamp: startDate.value
      ? Math.floor(localDayStartMs(startDate.value) / 1000)
      : 0,
    end_timestamp: endDate.value
      ? Math.floor(localDayEndMs(endDate.value) / 1000)
      : 0,
  }
}

const listRequest = useLatestRequest()

async function load(): Promise<void> {
  loading.value = true
  loadError.value = false
  // Parse inside the request so an invalid page takes the error branch.
  const result = await listRequest.run((signal) =>
    api
      .get<unknown>(endpoint(), currentParams(), { signal })
      .then(parseAuditLogPage)
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    rows.value = []
    total.value = 0
    loadError.value = true
    toast.error(
      result.error instanceof ApiError
        ? result.error.message
        : t('auditLogs.loadFailed')
    )
    return
  }
  rows.value = result.value.items
  total.value = result.value.total
}

let searchTimer = 0
function reload(): void {
  window.clearTimeout(searchTimer)
  if (page.value === 1) void load()
  else page.value = 1
}

watch([requestId, tokenRef], () => {
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reload, 300)
})
watch([category, outcome, startDate, endDate], reload)
watch(scope, (value) => {
  void router.replace({
    query: { ...route.query, scope: value === 'all' ? 'all' : undefined },
  })
  reload()
})
// The view is reused when the URL changes in place (tab link, back button).
watch(
  () => route.query.scope,
  (value) => {
    scope.value = value === 'all' && canReadAll.value ? 'all' : 'self'
  }
)
watch(pageSize, reload)
watch(page, load)

function actorName(log: AuditLogItem): string {
  return (
    log.actor.username ||
    (log.actor.id ? t('auditLogs.actorId', { id: log.actor.id }) : '-')
  )
}

function authMethod(log: AuditLogItem): string {
  if (!log.actor.auth_method) return '-'
  const key = auditLogAuthMethodKey(log.actor.auth_method)
  if (!key) return log.actor.auth_method
  const provider = log.actor.auth_method.startsWith('oauth:')
    ? log.actor.auth_method.slice(6)
    : ''
  return t(key, { provider })
}

function statusText(log: AuditLogItem): string {
  return log.request.status === null ? '-' : String(log.request.status)
}

const EXPORT_PAGE_SIZE = 100
const EXPORT_MAX_ROWS = 10_000
let exportController: AbortController | null = null

async function fetchAllLogs(
  signal: AbortSignal
): Promise<{ items: AuditLogItem[]; truncated: boolean }> {
  const { p: _page, page_size: _pageSize, ...filters } = currentParams()
  const firstRaw = await api.get<PageResult<AuditLogItem>>(
    endpoint(),
    { ...filters, p: 1, page_size: EXPORT_PAGE_SIZE },
    { signal }
  )
  const first = parseAuditLogPage(firstRaw as unknown)
  const items = [...first.items]
  const reachable = Math.min(first.total, EXPORT_MAX_ROWS)
  const pages = Math.ceil(reachable / EXPORT_PAGE_SIZE)

  for (let nextPage = 2; nextPage <= pages; nextPage++) {
    const nextRaw = await api.get<PageResult<AuditLogItem>>(
      endpoint(),
      { ...filters, p: nextPage, page_size: EXPORT_PAGE_SIZE },
      { signal }
    )
    const next = parseAuditLogPage(nextRaw as unknown)
    items.push(...next.items)
  }

  return {
    items: items.slice(0, EXPORT_MAX_ROWS),
    truncated: first.total > EXPORT_MAX_ROWS,
  }
}

function exportValues(log: AuditLogItem): readonly unknown[] {
  return [
    formatTime(log.created_at),
    actorName(log),
    t(auditLogRoleKey(log.actor.role)),
    auditLogCategoryLabel(log.category, t),
    t(auditLogResultKey(log)),
    auditLogSummary(log, t),
    log.ip,
    authMethod(log),
    log.user_agent,
    log.request.method,
    log.request.route,
    statusText(log),
    log.token_ref,
    log.request_id,
    Object.entries(log.params)
      .map(
        ([key, value]) =>
          `${auditLogParamLabel(key, t)}: ${auditLogParamValue(value)}`
      )
      .join('; '),
  ]
}

function download(content: string, mime: string, ext: string): void {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `ren2hub-audit-logs-${new Date().toISOString().slice(0, 10)}.${ext}`
  anchor.click()
  URL.revokeObjectURL(url)
}

async function doExport(): Promise<void> {
  exportController?.abort()
  const controller = new AbortController()
  exportController = controller
  exporting.value = true
  try {
    const { items, truncated } = await fetchAllLogs(controller.signal)
    if (exportType.value === 'json') {
      download(
        JSON.stringify(items, null, 2),
        'application/json;charset=utf-8',
        'json'
      )
    } else {
      const headers = [
        t('auditLogs.export.time'),
        t('auditLogs.export.actor'),
        t('auditLogs.export.role'),
        t('auditLogs.export.category'),
        t('auditLogs.export.result'),
        t('auditLogs.export.summary'),
        t('auditLogs.export.ip'),
        t('auditLogs.export.authMethod'),
        t('auditLogs.export.userAgent'),
        t('auditLogs.export.method'),
        t('auditLogs.export.route'),
        t('auditLogs.export.status'),
        t('auditLogs.export.tokenRef'),
        t('auditLogs.export.requestId'),
        t('auditLogs.export.params'),
      ]
      download(
        ...serializeSpreadsheet(
          headers,
          items.map(exportValues),
          exportType.value === 'excel' ? 'excel' : 'csv'
        )
      )
    }
    exportOpen.value = false
    if (truncated) {
      toast.warning(
        t('logs.exportTruncated', {
          count: items.length,
          limit: EXPORT_MAX_ROWS,
        })
      )
    } else {
      toast.success(t('logs.exported', { count: items.length }))
    }
  } catch (error) {
    if (controller.signal.aborted) return
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    if (exportController === controller) exportController = null
    exporting.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => {
  window.clearTimeout(searchTimer)
  exportController?.abort()
})
</script>

<template>
  <div class="mx-auto max-w-[var(--console-list-width)]">
    <PageBreadcrumb
      :crumbs="[t('logs.breadcrumb.0'), t('auditLogs.breadcrumb')]"
    >
      <template #action>
        <LogsNavTabs active="audit" />
      </template>
    </PageBreadcrumb>

    <div
      class="mb-3 flex flex-wrap items-center gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4"
    >
      <SearchInput
        v-model="requestId"
        maxlength="64"
        :placeholder="t('auditLogs.requestIdPlaceholder')"
        :aria-label="t('auditLogs.requestIdPlaceholder')"
        name="audit-log-request-id"
        class="w-full sm:w-64"
      />
      <FilterSelect
        v-if="canReadAll"
        v-model="scope"
        :options="scopeOptions"
        :label="t('auditLogs.scopeLabel')"
        :prefix-label="t('auditLogs.scopeLabel') + ':'"
        class="w-full sm:w-44"
      />
      <FilterSelect
        v-model="category"
        :options="categoryOptions"
        :label="t('auditLogs.categoryLabel')"
        :prefix-label="t('auditLogs.categoryLabel') + ':'"
        class="w-full sm:w-48"
      />
      <FilterSelect
        v-model="outcome"
        :options="outcomeOptions"
        :label="t('auditLogs.resultLabel')"
        :prefix-label="t('auditLogs.resultLabel') + ':'"
        class="w-full sm:w-40"
      />
      <DateRangePicker
        v-model:start="startDate"
        v-model:end="endDate"
        class="w-full sm:w-64"
      />
      <SearchInput
        v-if="canReadAll && scope === 'all'"
        v-model="tokenRef"
        maxlength="64"
        :placeholder="t('auditLogs.tokenRefPlaceholder')"
        :aria-label="t('auditLogs.tokenRefPlaceholder')"
        name="audit-log-token-ref"
        class="w-full sm:w-56"
      />
      <ConsoleButton
        class="ml-auto w-full sm:w-auto"
        variant="secondary"
        @click="exportOpen = true"
      >
        <Download :size="16" aria-hidden="true" />
        {{ t('common.export') }}
      </ConsoleButton>
    </div>

    <ConsoleCard v-if="loadError && !loading" :padded="false">
      <EmptyState
        :title="t('auditLogs.loadFailed')"
        :hint="t('auditLogs.loadFailedHint')"
        illustration="empty-logs"
      >
        <ConsoleButton class="mt-5" variant="secondary" @click="load">
          {{ t('common.retry') }}
        </ConsoleButton>
      </EmptyState>
    </ConsoleCard>

    <ConsoleCard v-else class="hidden lg:block" :padded="false">
      <DataTable
        :columns="columns"
        :rows="rows"
        row-key="event_id"
        :loading="loading"
        :skeleton-rows="pageSize"
        adaptive-scroll
        :page-size="pageSize"
        min-table-width="1120px"
        :scroll-region-label="t('auditLogs.breadcrumb')"
        :empty-title="t('auditLogs.emptyTitle')"
        :empty-hint="t('auditLogs.emptyHint')"
        empty-illustration="empty-logs"
      >
        <template #cell-created="{ row }">
          <span class="whitespace-nowrap text-xs text-[var(--text-tertiary)]">
            {{ formatTime((row as AuditLogItem).created_at) }}
          </span>
        </template>
        <template #cell-actor="{ row }">
          <div class="min-w-0">
            <p
              class="truncate text-sm font-semibold text-[var(--text-primary)]"
              :title="actorName(row as AuditLogItem)"
            >
              {{ actorName(row as AuditLogItem) }}
            </p>
            <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
              {{ t(auditLogRoleKey((row as AuditLogItem).actor.role)) }}
            </p>
          </div>
        </template>
        <template #cell-result="{ row }">
          <div class="flex flex-wrap items-center gap-1.5">
            <StatusChip
              :tone="auditLogCategoryTone((row as AuditLogItem).category)"
            >
              {{ auditLogCategoryLabel((row as AuditLogItem).category, t) }}
            </StatusChip>
            <StatusChip :tone="auditLogResultTone(row as AuditLogItem)">
              {{ t(auditLogResultKey(row as AuditLogItem)) }}
            </StatusChip>
          </div>
        </template>
        <template #cell-summary="{ row }">
          <p
            class="line-clamp-2 text-sm leading-5 text-[var(--text-secondary)]"
            :title="auditLogSummary(row as AuditLogItem, t)"
          >
            {{ auditLogSummary(row as AuditLogItem, t) }}
          </p>
        </template>
        <template #cell-source="{ row }">
          <span
            class="block truncate font-mono text-xs text-[var(--text-secondary)]"
            :title="(row as AuditLogItem).ip || '-'"
          >
            {{ (row as AuditLogItem).ip || '-' }}
          </span>
        </template>
        <template #cell-request="{ row }">
          <div class="min-w-0">
            <p
              class="truncate font-mono text-xs text-[var(--text-secondary)]"
              :title="auditLogRequestText(row as AuditLogItem) || '-'"
            >
              {{ auditLogRequestText(row as AuditLogItem) || '-' }}
            </p>
            <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
              {{
                t('auditLogs.httpStatus', {
                  status: statusText(row as AuditLogItem),
                })
              }}
            </p>
          </div>
        </template>
        <template #cell-detail="{ row }">
          <div class="flex justify-end">
            <IconButton
              :label="t('auditLogs.viewDetails')"
              @click="detailLog = row as AuditLogItem"
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
      :aria-label="t('auditLogs.breadcrumb')"
    >
      <div v-if="loading" class="sibling-space-y-3" aria-busy="true">
        <SkeletonBlock
          v-for="index in Math.min(pageSize, 5)"
          :key="index"
          bordered
          rounded="xl"
          class="h-48"
        />
      </div>
      <div v-else-if="rows.length" class="sibling-space-y-3">
        <article
          v-for="row in rows"
          :key="row.event_id"
          class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4"
          :aria-label="
            t('auditLogs.mobileCardLabel', {
              action: auditLogSummary(row, t),
            })
          "
        >
          <div class="flex flex-wrap items-center gap-1.5">
            <StatusChip :tone="auditLogCategoryTone(row.category)">
              {{ auditLogCategoryLabel(row.category, t) }}
            </StatusChip>
            <StatusChip :tone="auditLogResultTone(row)">
              {{ t(auditLogResultKey(row)) }}
            </StatusChip>
          </div>
          <div class="mt-3 min-w-0">
            <p
              class="truncate text-sm font-semibold text-[var(--text-primary)]"
            >
              {{ actorName(row) }}
            </p>
            <p
              class="mt-1 line-clamp-3 text-sm leading-5 text-[var(--text-secondary)]"
            >
              {{ auditLogSummary(row, t) }}
            </p>
          </div>
          <div
            class="mt-4 flex items-end justify-between gap-3 border-t border-[var(--border-subtle)] pt-3"
          >
            <div class="min-w-0 text-xs text-[var(--text-tertiary)]">
              <p class="truncate font-mono" :title="row.ip || '-'">
                {{ row.ip || '-' }}
              </p>
              <p class="mt-1">{{ formatTime(row.created_at) }}</p>
            </div>
            <ConsoleButton size="sm" variant="ghost" @click="detailLog = row">
              <Eye :size="15" aria-hidden="true" />
              {{ t('auditLogs.viewDetails') }}
            </ConsoleButton>
          </div>
        </article>
      </div>
      <div
        v-else
        class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <EmptyState
          :title="t('auditLogs.emptyTitle')"
          :hint="t('auditLogs.emptyHint')"
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
      :title="t('auditLogs.detailTitle')"
      size="lg"
      @close="detailLog = null"
    >
      <div v-if="detailLog" class="sibling-space-y-6" data-audit-log-detail>
        <section>
          <h3 class="text-sm font-semibold text-[var(--text-primary)]">
            {{ t('auditLogs.sections.actor') }}
          </h3>
          <dl class="mt-3 grid gap-3 sm:grid-cols-2">
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.account') }}
              </dt>
              <dd class="mt-1 break-words text-sm text-[var(--text-primary)]">
                {{ actorName(detailLog) }}
              </dd>
            </div>
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.role') }}
              </dt>
              <dd class="mt-1 text-sm text-[var(--text-primary)]">
                {{ t(auditLogRoleKey(detailLog.actor.role)) }}
              </dd>
            </div>
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.authMethod') }}
              </dt>
              <dd class="mt-1 break-words text-sm text-[var(--text-primary)]">
                {{ authMethod(detailLog) }}
              </dd>
            </div>
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.time') }}
              </dt>
              <dd class="mt-1 text-sm text-[var(--text-primary)]">
                {{ formatTime(detailLog.created_at) }}
              </dd>
            </div>
          </dl>
        </section>

        <section>
          <h3 class="text-sm font-semibold text-[var(--text-primary)]">
            {{ t('auditLogs.sections.operation') }}
          </h3>
          <div class="mt-3 flex flex-wrap items-center gap-1.5">
            <StatusChip :tone="auditLogCategoryTone(detailLog.category)">
              {{ auditLogCategoryLabel(detailLog.category, t) }}
            </StatusChip>
          </div>
          <p
            class="mt-3 break-words rounded-lg bg-[var(--surface-muted)] p-3 text-sm leading-6 text-[var(--text-primary)]"
          >
            {{ auditLogSummary(detailLog, t) }}
          </p>
          <dl v-if="detailParams.length" class="mt-3 grid gap-2 sm:grid-cols-2">
            <div
              v-for="[key, value] in detailParams"
              :key="key"
              class="min-w-0 rounded-lg border border-[var(--border-subtle)] p-3"
            >
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ auditLogParamLabel(key, t) }}
              </dt>
              <dd
                class="mt-1 break-words font-mono text-xs text-[var(--text-primary)]"
              >
                {{ auditLogParamValue(value) }}
              </dd>
            </div>
          </dl>
        </section>

        <section>
          <h3 class="text-sm font-semibold text-[var(--text-primary)]">
            {{ t('auditLogs.sections.source') }}
          </h3>
          <dl class="mt-3 sibling-space-y-3">
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.ip') }}
              </dt>
              <dd
                class="mt-1 break-all font-mono text-xs text-[var(--text-primary)]"
              >
                {{ detailLog.ip || '-' }}
              </dd>
            </div>
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.userAgent') }}
              </dt>
              <dd
                class="mt-1 break-words text-xs leading-5 text-[var(--text-primary)]"
              >
                {{ detailLog.user_agent || '-' }}
              </dd>
            </div>
            <div
              v-if="detailLog.token_ref"
              class="rounded-lg bg-[var(--surface-muted)] p-3"
            >
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.tokenRef') }}
              </dt>
              <dd
                class="mt-1 break-all font-mono text-xs text-[var(--text-primary)]"
              >
                {{ detailLog.token_ref }}
              </dd>
            </div>
          </dl>
        </section>

        <section>
          <h3 class="text-sm font-semibold text-[var(--text-primary)]">
            {{ t('auditLogs.sections.request') }}
          </h3>
          <dl class="mt-3 grid gap-3 sm:grid-cols-2">
            <div class="rounded-lg bg-[var(--surface-muted)] p-3 sm:col-span-2">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.route') }}
              </dt>
              <dd
                class="mt-1 break-all font-mono text-xs text-[var(--text-primary)]"
              >
                {{ auditLogRequestText(detailLog) || '-' }}
              </dd>
            </div>
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.httpStatus') }}
              </dt>
              <dd class="mt-1 text-sm text-[var(--text-primary)]">
                {{ statusText(detailLog) }}
              </dd>
            </div>
            <div class="rounded-lg bg-[var(--surface-muted)] p-3">
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.result') }}
              </dt>
              <dd class="mt-1">
                <StatusChip :tone="auditLogResultTone(detailLog)">{{
                  t(auditLogResultKey(detailLog))
                }}</StatusChip>
              </dd>
            </div>
            <div
              v-if="detailLog.request_id"
              class="rounded-lg bg-[var(--surface-muted)] p-3 sm:col-span-2"
            >
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ t('auditLogs.fields.requestId') }}
              </dt>
              <dd
                class="mt-1 break-all font-mono text-xs text-[var(--text-primary)]"
              >
                {{ detailLog.request_id }}
              </dd>
            </div>
          </dl>
        </section>
      </div>
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
      :subtitle="t('auditLogs.exportSubtitle', { limit: EXPORT_MAX_ROWS })"
      size="sm"
      @close="exportOpen = false"
    >
      <FormField :label="t('logs.docType')">
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
        <ConsoleButton size="lg" block :loading="exporting" @click="doExport">
          {{ t('common.confirm') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>
  </div>
</template>
