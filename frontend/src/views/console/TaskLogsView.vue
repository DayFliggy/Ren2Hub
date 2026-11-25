<script setup lang="ts">
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, routeLocationKey, routerKey } from 'vue-router'

import { api } from '@/api/console'
import { parseTaskLogPage } from '@/api/liveContracts'
import { ApiError } from '@/api/types'
import type { RelayTaskLogItem } from '@/types/console'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import AsyncTaskSubTabs from '@/components/console/log-ui/AsyncTaskSubTabs.vue'
import LogsNavTabs from '@/components/console/log-ui/LogsNavTabs.vue'
import {
  TASK_PLATFORM_PRESETS,
  taskPlatformLabel,
} from '@/components/console/log-ui/logClassification'
import {
  relayTaskDurationSeconds,
  relayTaskStatusMeta,
} from '@/components/console/log-ui/relayTaskStatus'
import TaskArtifactsCell from '@/components/console/log-ui/TaskArtifactsCell.vue'
import { taskPreviewMode } from '@/components/console/log-ui/taskArtifacts'
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

const { t } = useI18n()
const auth = useAuthStore()
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)
const hasRouter = Boolean(router)

const canReadAll = computed(() => auth.isAdmin)

function queryStringParam(name: string): string {
  const value = route?.query?.[name]
  return typeof value === 'string' ? value : ''
}

/** Gateway fail reasons are fixed English sentences; provider text passes through. */
function failReason(row: { fail_reason: string }): string {
  return translateServerText(getLocale(), row.fail_reason)
}
const toast = useToast()

const CUSTOM_PLATFORM_VALUE = '__custom__'

const rows = ref<RelayTaskLogItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(true)
const loadError = ref<string | null>(null)

const taskId = ref(queryStringParam('task_id'))
const platformPreset = ref('')
const customPlatform = ref('')
const status = ref('')
const channelId = ref('')
const scope = ref<'self' | 'all'>(
  queryStringParam('scope') === 'all' && canReadAll.value ? 'all' : 'self'
)
const readingAll = computed(() => scope.value === 'all' && canReadAll.value)
const startDate = ref('')
const endDate = ref('')

const failureRow = ref<RelayTaskLogItem | null>(null)

const scopeOptions = computed(() => [
  { value: 'self', label: t('relayLogs.scopeSelf') },
  { value: 'all', label: t('relayLogs.scopeAll') },
])

const platformOptions = computed(() => [
  { value: '', label: t('relayLogs.platformAll') },
  ...TASK_PLATFORM_PRESETS.map((item) => ({
    value: item.value,
    label: item.label,
  })),
  { value: CUSTOM_PLATFORM_VALUE, label: t('relayLogs.platformCustom') },
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
    value: 'NOT_START',
    label: t('relayLogs.statusNotStart'),
  },
  {
    value: 'FAILURE',
    label: t('relayLogs.statusFailed'),
    tone: 'danger' as const,
  },
])

const columns = computed<TableColumn[]>(() => [
  { key: 'submitted', label: t('relayLogs.colSubmitted'), width: '170px' },
  ...(readingAll.value
    ? [{ key: 'account', label: t('relayLogs.colAccount'), width: '140px' }]
    : []),
  { key: 'task', label: t('relayLogs.colTask'), width: '216px' },
  { key: 'platform', label: t('relayLogs.colPlatform'), width: '128px' },
  { key: 'progress', label: t('relayLogs.colProgress'), width: '124px' },
  { key: 'result', label: t('relayLogs.colResult') },
  {
    key: 'cost',
    label: t('relayLogs.colCost'),
    width: '100px',
    align: 'right',
  },
])

const resolvedPlatform = computed(() => {
  if (platformPreset.value === CUSTOM_PLATFORM_VALUE) {
    return customPlatform.value.trim()
  }
  return platformPreset.value.trim()
})

function endpoint(): '/api/task/self' | '/api/task' {
  return readingAll.value ? '/api/task' : '/api/task/self'
}

function currentParams() {
  // /api/task filters compare against second-precision submit_time values;
  // an unset date is omitted, matching the drawing log page.
  const startTimestamp = startDate.value
    ? Math.floor(localDayStartMs(startDate.value) / 1000)
    : undefined
  const endTimestamp = endDate.value
    ? Math.floor(localDayEndMs(endDate.value) / 1000)
    : undefined
  const platformVal = resolvedPlatform.value
  const statusVal = status.value.trim()
  const channelVal = readingAll.value ? channelId.value.trim() : ''
  return {
    p: page.value,
    page_size: pageSize.value,
    task_id: taskId.value.trim(),
    platform: platformVal || undefined,
    status: statusVal || undefined,
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
    parseTaskLogPage(
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
watch([taskId, customPlatform, channelId], () => {
  // The input already describes a new query during the debounce interval.
  listRequest.cancel()
  clearResults()
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reload, 300)
})
watch([platformPreset, status, startDate, endDate], reload)
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
    () => route.query.task_id,
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

function formatDuration(row: RelayTaskLogItem): string {
  const seconds = relayTaskDurationSeconds(
    row.submit_time,
    row.finish_time,
    'seconds'
  )
  return seconds === null ? '-' : `${seconds.toFixed(0)}s`
}
</script>

<template>
  <div class="mx-auto max-w-[var(--console-list-width)]">
    <PageBreadcrumb
      :crumbs="[t('logs.breadcrumb.0'), t('relayLogs.tasksBreadcrumb')]"
    >
      <template #action>
        <LogsNavTabs active="tasks" />
      </template>
    </PageBreadcrumb>

    <AsyncTaskSubTabs active="general" :scope-all="readingAll" />

    <!-- filter toolbar -->
    <div
      class="mb-3 flex flex-wrap items-center gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4"
    >
      <SearchInput
        v-model="taskId"
        :placeholder="t('relayLogs.taskIdPlaceholder')"
        :aria-label="t('relayLogs.taskIdPlaceholder')"
        name="task-log-search"
        class="w-full sm:w-56"
      />
      <FilterSelect
        v-if="canReadAll"
        v-model="scope"
        :options="scopeOptions"
        :label="t('relayLogs.scopeLabel')"
        :prefix-label="t('relayLogs.scopeLabel') + ':'"
        class="w-full sm:w-48"
      />
      <FilterSelect
        v-model="platformPreset"
        :options="platformOptions"
        :label="t('relayLogs.platformLabel')"
        :prefix-label="t('relayLogs.platformLabel') + ':'"
        class="w-full sm:w-52"
      />
      <SearchInput
        v-if="platformPreset === CUSTOM_PLATFORM_VALUE"
        v-model="customPlatform"
        :placeholder="t('relayLogs.platformPlaceholder')"
        :aria-label="t('relayLogs.platformPlaceholder')"
        name="task-log-platform"
        class="w-full sm:w-52"
      />
      <FilterSelect
        v-model="status"
        :options="statusOptions"
        :label="t('relayLogs.statusLabel')"
        :prefix-label="t('relayLogs.statusLabel') + ':'"
        class="w-full sm:w-44"
      />
      <SearchInput
        v-if="readingAll"
        v-model="channelId"
        :placeholder="t('relayLogs.channelIdPlaceholder')"
        :aria-label="t('relayLogs.channelIdPlaceholder')"
        name="task-log-channel"
        class="w-full sm:w-36"
      />
      <DateRangePicker
        v-model:start="startDate"
        v-model:end="endDate"
        class="w-full sm:w-64"
      />
    </div>

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
        min-table-width="940px"
        :scroll-region-label="t('relayLogs.tasksBreadcrumb')"
        :empty-title="t('relayLogs.emptyTitle')"
        :empty-hint="t('relayLogs.emptyHint')"
        empty-illustration="empty-logs"
      >
        <template #cell-submitted="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-1.5">
            <span class="whitespace-nowrap text-xs text-[var(--text-tertiary)]">
              {{ formatTime((row as RelayTaskLogItem).submit_time) }}
            </span>
            <StatusChip
              :tone="relayTaskStatusMeta((row as RelayTaskLogItem).status).tone"
            >
              {{
                t(
                  relayTaskStatusMeta((row as RelayTaskLogItem).status).labelKey
                )
              }}
            </StatusChip>
          </div>
        </template>
        <template #cell-account="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-0.5">
            <span
              class="block max-w-full truncate text-xs font-semibold text-[var(--text-primary)]"
            >
              {{
                (row as RelayTaskLogItem).username ||
                ((row as RelayTaskLogItem).user_id
                  ? t('relayLogs.userIdBadge', {
                      id: (row as RelayTaskLogItem).user_id,
                    })
                  : '-')
              }}
            </span>
            <span
              v-if="(row as RelayTaskLogItem).channel_id"
              class="font-mono text-[0.6875rem] text-[var(--text-tertiary)]"
            >
              {{
                t('relayLogs.channelIdBadge', {
                  id: (row as RelayTaskLogItem).channel_id,
                })
              }}
            </span>
          </div>
        </template>
        <template #cell-task="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-1">
            <span
              v-if="(row as RelayTaskLogItem).origin_model_name"
              class="block max-w-full truncate font-mono text-xs font-semibold text-[var(--text-primary)]"
              :title="(row as RelayTaskLogItem).origin_model_name"
            >
              {{ (row as RelayTaskLogItem).origin_model_name }}
            </span>
            <span
              class="text-xs"
              :class="
                (row as RelayTaskLogItem).origin_model_name
                  ? 'text-[var(--text-secondary)]'
                  : 'font-semibold text-[var(--text-primary)]'
              "
              :title="(row as RelayTaskLogItem).action"
            >
              {{ (row as RelayTaskLogItem).action || '-' }}
            </span>
            <span
              class="block max-w-full truncate font-mono text-xs text-[var(--text-tertiary)]"
              :title="(row as RelayTaskLogItem).task_id"
            >
              {{ (row as RelayTaskLogItem).task_id || '-' }}
            </span>
            <RouterLink
              v-if="
                hasRouter && (row as RelayTaskLogItem).admin_info?.request_id
              "
              :to="{
                name: 'logs',
                query: {
                  request_id: (row as RelayTaskLogItem).admin_info!.request_id!,
                  ...(readingAll ? { scope: 'all' } : {}),
                },
              }"
              class="text-[0.6875rem] text-[var(--accent-text)] underline-offset-2 hover:underline"
            >
              {{ t('relayLogs.openCallLog') }}
            </RouterLink>
          </div>
        </template>
        <template #cell-platform="{ row }">
          <StatusChip
            tone="info"
            :title="(row as RelayTaskLogItem).platform || '-'"
          >
            {{ taskPlatformLabel((row as RelayTaskLogItem).platform) }}
          </StatusChip>
        </template>
        <template #cell-progress="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-1">
            <span class="text-xs text-[var(--text-secondary)]">
              {{ (row as RelayTaskLogItem).progress || '-' }}
            </span>
            <span class="text-xs text-[var(--text-tertiary)]">
              {{ formatDuration(row as RelayTaskLogItem) }}
            </span>
          </div>
        </template>
        <template #cell-result="{ row }">
          <button
            v-if="(row as RelayTaskLogItem).fail_reason"
            type="button"
            class="block max-w-[17.5rem] truncate text-left text-xs text-[var(--status-danger-text)] underline-offset-2 hover:underline"
            :title="t('relayLogs.viewDetails')"
            @click="failureRow = row as RelayTaskLogItem"
          >
            {{ failReason(row as RelayTaskLogItem) }}
          </button>
          <TaskArtifactsCell v-else :task="row as RelayTaskLogItem" />
        </template>
        <template #cell-cost="{ row }">
          <span
            class="whitespace-nowrap text-sm font-semibold tabular-nums text-[var(--text-primary)]"
          >
            {{ formatQuota((row as RelayTaskLogItem).quota) }}
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
    <section class="lg:hidden" :aria-label="t('relayLogs.tasksBreadcrumb')">
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
            <div class="flex flex-wrap items-center gap-1.5">
              <StatusChip :tone="relayTaskStatusMeta(row.status).tone">
                {{ t(relayTaskStatusMeta(row.status).labelKey) }}
              </StatusChip>
              <StatusChip tone="info" :title="row.platform || '-'">
                {{ taskPlatformLabel(row.platform) }}
              </StatusChip>
            </div>
            <span class="text-xs text-[var(--text-tertiary)]">
              {{ formatTime(row.submit_time) }}
            </span>
          </div>
          <p
            v-if="readingAll && (row.username || row.user_id || row.channel_id)"
            class="mt-1.5 truncate text-xs text-[var(--text-secondary)]"
          >
            {{
              row.username ||
              (row.user_id
                ? t('relayLogs.userIdBadge', { id: row.user_id })
                : '')
            }}
            <template v-if="row.channel_id">
              · {{ t('relayLogs.channelIdBadge', { id: row.channel_id }) }}
            </template>
          </p>
          <p
            v-if="row.origin_model_name"
            class="mt-2 truncate font-mono text-sm font-semibold text-[var(--text-primary)]"
          >
            {{ row.origin_model_name }}
          </p>
          <p
            class="mt-1 truncate font-mono text-xs text-[var(--text-tertiary)]"
          >
            {{ taskPlatformLabel(row.platform) }} · {{ row.action || '-' }} ·
            {{ row.task_id || '-' }}
          </p>
          <button
            v-if="row.fail_reason"
            type="button"
            class="mt-2 block w-full truncate text-left text-sm text-[var(--status-danger-text)]"
            @click="failureRow = row"
          >
            {{ failReason(row) }}
          </button>
          <div class="mt-3 flex items-center justify-between gap-2">
            <span class="text-xs text-[var(--text-tertiary)]">
              {{ row.progress || '-' }} · {{ formatDuration(row) }}
            </span>
            <span class="text-sm font-semibold tabular-nums">
              {{ formatQuota(row.quota) }}
            </span>
          </div>
          <div
            v-if="!row.fail_reason && taskPreviewMode(row) !== 'none'"
            class="mt-2"
          >
            <TaskArtifactsCell :task="row" />
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

    <!-- failure details -->
    <ConsoleModal
      :open="failureRow !== null"
      :title="t('relayLogs.failReason')"
      size="md"
      @close="failureRow = null"
    >
      <p
        class="whitespace-pre-wrap break-words text-sm text-[var(--status-danger-text)]"
      >
        {{ failureRow ? failReason(failureRow) : '' }}
      </p>
      <template #footer>
        <div class="flex justify-end">
          <ConsoleButton variant="secondary" @click="failureRow = null">
            {{ t('common.close') }}
          </ConsoleButton>
        </div>
      </template>
    </ConsoleModal>
  </div>
</template>
