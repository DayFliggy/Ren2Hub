<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { MoreHorizontal, Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import {
  systemManagementApi,
  type SystemTask,
  type SystemTaskHealth,
} from '@/api/systemManagement'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import SystemTaskSection from '@/components/admin/SystemTaskSection.vue'
import { useSystemManagement } from '@/composables/useSystemManagement'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { systemManagementMessages } from '@/i18n/systemManagement'
import {
  durationParts,
  formatRelative,
  formatTimestamp,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import '@/components/admin/admin.css'

const HEALTH_TONES = {
  ok: 'success',
  failing: 'danger',
  stale: 'warning',
  idle: 'neutral',
  running: 'info',
  inactive: 'neutral',
} as const satisfies Record<SystemTaskHealth, string>

// One local composer per component: Vue I18n reuses the first local scope.
const { t, te, locale } = useI18n({
  useScope: 'local',
  messages: {
    'zh-CN': {
      ...systemManagementMessages['zh-CN'],
      ops: opsMonitorMessages['zh-CN'],
    },
    en: { ...systemManagementMessages.en, ops: opsMonitorMessages.en },
  },
})
const {
  summary,
  active,
  history,
  activeTasks,
  historyTasks,
  taskTypes,
  historyType,
  historyStatus,
  historyPage,
  historyPageSize,
  historyTotal,
} = useSystemManagement()

const typeLabel = (type: string) => (te(type) ? t(type) : type)
const date = (stamp: number | null) => formatTimestamp(stamp, locale.value)
// The cleanup boundary is picked to the second, so confirm it to the second.
const cleanupDate = (stamp: number) =>
  new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(stamp * 1000)
const interval = (seconds: number) =>
  durationParts(seconds)
    .map((part) => t(`ops.duration.${part.unit}`, { n: part.amount }))
    .join(' ')

const result = ref('')
const taskDetail = ref<SystemTask | null>(null)
let disposed = false
onBeforeUnmount(() => {
  disposed = true
})

const historyTarget = ref<{
  type: string
  status: '' | 'succeeded' | 'failed'
} | null>(null)
const clearingHistory = ref(false)
const historyDeleteError = ref('')
const historyDeleteMessage = computed(() =>
  t('deleteHistoryMessage', {
    type: historyTarget.value?.type
      ? typeLabel(historyTarget.value.type)
      : t('allTypes'),
    status: historyTarget.value?.status
      ? t(historyTarget.value.status)
      : t('allStatuses'),
  })
)

function confirmHistoryCleanup() {
  historyDeleteError.value = ''
  historyTarget.value = { type: historyType.value, status: historyStatus.value }
}

async function deleteHistory() {
  if (!historyTarget.value || clearingHistory.value) return
  clearingHistory.value = true
  historyDeleteError.value = ''
  try {
    const count = await systemManagementApi.deleteHistory(historyTarget.value)
    if (disposed) return
    result.value = t('historyDeleted', { count })
    historyTarget.value = null
    if (historyPage.value !== 1) historyPage.value = 1
    else await history.refresh()
  } catch (cause) {
    if (!disposed)
      historyDeleteError.value =
        cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) clearingHistory.value = false
  }
}

const logDialog = ref(false)
const logMenu = ref<HTMLDetailsElement | null>(null)
const before = ref('')
const logTarget = ref<number | null>(null)
const creating = ref(false)
const logError = ref('')

function openLogDialog() {
  if (logMenu.value) logMenu.value.open = false
  logError.value = ''
  logDialog.value = true
}

function confirmLogCleanup() {
  const timestamp = Date.parse(before.value)
  if (
    !Number.isFinite(timestamp) ||
    timestamp <= 0 ||
    timestamp >= Date.now()
  ) {
    logError.value = t('invalidDate')
    return
  }
  logError.value = ''
  logTarget.value = Math.floor(timestamp / 1000)
}

async function cleanLogs() {
  if (!logTarget.value || creating.value) return
  creating.value = true
  logError.value = ''
  result.value = ''
  try {
    const task = await systemManagementApi.cleanup(logTarget.value)
    if (disposed) return
    result.value = t('cleanupCreated', { id: task.task_id })
    logTarget.value = null
    logDialog.value = false
    await Promise.all([active.refresh(), history.refresh(), summary.refresh()])
  } catch (cause) {
    if (!disposed)
      logError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) creating.value = false
  }
}
</script>

<template>
  <OpsCard
    :title="t('ops.tasks.title')"
    :scope="t('ops.rootOnly')"
    :loading="summary.loading.value"
    :loaded="summary.data.value !== null"
    :error="summary.error.value"
    :updated-at="summary.updatedAt.value"
    data-ops-section="tasks"
    @retry="summary.refresh"
  >
    <p v-if="result" class="ops-result" role="status">{{ result }}</p>
    <p v-if="!summary.data.value?.length" class="ops-empty">
      {{ t('ops.tasks.empty') }}
    </p>
    <ul v-else class="ops-task-types" data-ops-task-summary>
      <li
        v-for="row in summary.data.value"
        :key="row.type"
        :data-task-type="row.type"
      >
        <div class="ops-task-head">
          <strong>{{ typeLabel(row.type) }}</strong>
          <StatusChip
            :tone="HEALTH_TONES[row.status]"
            :title="
              row.status === 'inactive'
                ? t('ops.tasks.inactiveHint')
                : undefined
            "
            >{{ t(`ops.tasks.health.${row.status}`) }}</StatusChip
          >
        </div>
        <p class="ops-task-meta">
          <code>{{ row.type }}</code>
          <span>{{
            row.scheduled && row.interval_seconds !== null
              ? t('ops.tasks.every', {
                  interval: interval(row.interval_seconds),
                })
              : t('ops.tasks.onDemand')
          }}</span>
          <span :title="date(row.last_success_at)"
            >{{ t('ops.tasks.lastSuccess') }}
            {{
              formatRelative(
                row.last_success_at,
                summary.updatedAt.value,
                locale
              )
            }}</span
          >
          <span v-if="row.last_failure_at" :title="date(row.last_failure_at)"
            >{{ t('ops.tasks.lastFailure') }}
            {{
              formatRelative(
                row.last_failure_at,
                summary.updatedAt.value,
                locale
              )
            }}</span
          >
        </p>
        <p v-if="row.last_error" class="ops-task-error" :title="row.last_error">
          {{ row.last_error }}
        </p>
      </li>
    </ul>

    <SystemTaskSection
      :title="t('activeTasks')"
      :empty-text="t('emptyActive')"
      :tasks="activeTasks"
      :loading="active.loading.value"
      :loaded="active.data.value !== null"
      :error="active.error.value"
      :updated-at="active.updatedAt.value"
      @refresh="active.refresh"
      @details="taskDetail = $event"
    >
      <template #actions
        ><details
          ref="logMenu"
          class="ops-menu"
          @keydown.esc="logMenu && (logMenu.open = false)"
        >
          <summary :aria-label="t('actions')" :title="t('actions')">
            <MoreHorizontal :size="18" />
          </summary>
          <div class="ops-menu-content">
            <button type="button" @click="openLogDialog">
              <Trash2 :size="15" />{{ t('cleanupLogs') }}
            </button>
          </div>
        </details></template
      >
    </SystemTaskSection>
    <SystemTaskSection
      :title="t('historyTasks')"
      :empty-text="t('emptyHistory')"
      :tasks="historyTasks"
      :total="historyTotal"
      collapsible
      data-ops-task-history
      :loading="history.loading.value"
      :loaded="history.data.value !== null"
      :error="history.error.value"
      :updated-at="history.updatedAt.value"
      @refresh="history.refresh"
      @details="taskDetail = $event"
    >
      <template #actions>
        <ConsoleButton
          variant="ghost"
          size="sm"
          :disabled="history.loading.value || !historyTotal || clearingHistory"
          @click="confirmHistoryCleanup"
        >
          <Trash2 :size="15" />{{ t('deleteHistory') }}
        </ConsoleButton>
      </template>
      <template #filters>
        <div class="ops-history-filters">
          <label class="admin-field"
            ><span>{{ t('filterType') }}</span
            ><select v-model="historyType" :disabled="clearingHistory">
              <option value="">{{ t('allTypes') }}</option>
              <option v-for="type in taskTypes" :key="type" :value="type">
                {{ typeLabel(type) }}
              </option>
            </select></label
          >
          <label class="admin-field"
            ><span>{{ t('filterStatus') }}</span
            ><select v-model="historyStatus" :disabled="clearingHistory">
              <option value="">{{ t('allStatuses') }}</option>
              <option value="succeeded">{{ t('succeeded') }}</option>
              <option value="failed">{{ t('failed') }}</option>
            </select></label
          >
        </div>
      </template>
      <template #footer>
        <TablePagination
          v-model:page="historyPage"
          v-model:page-size="historyPageSize"
          :total="historyTotal"
        />
      </template>
    </SystemTaskSection>
  </OpsCard>

  <ConsoleModal
    :open="taskDetail !== null"
    :title="t('taskDetails')"
    size="lg"
    @close="taskDetail = null"
  >
    <template v-if="taskDetail"
      ><dl class="admin-values">
        <div>
          <dt>{{ t('type') }}</dt>
          <dd>{{ typeLabel(taskDetail.type) }}</dd>
        </div>
        <div>
          <dt>{{ t('status') }}</dt>
          <dd>{{ t(taskDetail.status) }}</dd>
        </div>
        <div>
          <dt>{{ t('taskId') }}</dt>
          <dd>{{ taskDetail.task_id }}</dd>
        </div>
        <div>
          <dt>{{ t('executor') }}</dt>
          <dd>{{ taskDetail.locked_by || '--' }}</dd>
        </div>
        <div>
          <dt>{{ t('created') }}</dt>
          <dd>{{ date(taskDetail.created_at) }}</dd>
        </div>
        <div>
          <dt>{{ t('updated') }}</dt>
          <dd>{{ date(taskDetail.updated_at) }}</dd>
        </div>
        <div>
          <dt>{{ t('processed') }}</dt>
          <dd>
            {{ taskDetail.processed ?? '--' }} / {{ taskDetail.total ?? '--' }}
          </dd>
        </div>
        <div>
          <dt>{{ t('deletedLogs') }}</dt>
          <dd>{{ taskDetail.deleted_count ?? '--' }}</dd>
        </div>
      </dl>
      <div v-if="taskDetail.error" class="ops-task-detail-error">
        <h3>{{ t('error') }}</h3>
        <pre class="admin-log admin-error">{{ taskDetail.error }}</pre>
      </div></template
    >
    <template #footer
      ><ConsoleButton variant="secondary" @click="taskDetail = null">{{
        t('cancel')
      }}</ConsoleButton></template
    >
  </ConsoleModal>
  <ConsoleModal
    :open="logDialog"
    :title="t('cleanupLogs')"
    :close-disabled="creating"
    @close="logDialog = false"
  >
    <form
      id="log-cleanup-form"
      class="admin-form"
      @submit.prevent="confirmLogCleanup"
    >
      <label class="admin-field"
        ><span>{{ t('cleanupBefore') }}</span
        ><input
          v-model="before"
          type="datetime-local"
          step="1"
          required
          :disabled="creating"
      /></label>
      <p v-if="logError && !logTarget" class="admin-error" role="alert">
        {{ logError }}
      </p>
    </form>
    <template #footer
      ><div class="admin-row-actions">
        <ConsoleButton
          type="submit"
          form="log-cleanup-form"
          :disabled="creating"
          >{{ t('continue') }}</ConsoleButton
        ><ConsoleButton
          variant="secondary"
          :disabled="creating"
          @click="logDialog = false"
          >{{ t('cancel') }}</ConsoleButton
        >
      </div></template
    >
  </ConsoleModal>
  <ConfirmDialog
    :open="logTarget !== null"
    :title="t('cleanupConfirm')"
    :message="
      t('cleanupMessage', { date: logTarget ? cleanupDate(logTarget) : '--' })
    "
    :confirm-text="t('remove')"
    :loading="creating"
    @confirm="cleanLogs"
    @cancel="logTarget = null"
    ><p v-if="logError" class="admin-error mt-3" role="alert">
      {{ logError }}
    </p></ConfirmDialog
  >
  <ConfirmDialog
    :open="historyTarget !== null"
    :title="t('deleteHistory')"
    :message="historyDeleteMessage"
    :confirm-text="t('remove')"
    :loading="clearingHistory"
    @confirm="deleteHistory"
    @cancel="historyTarget = null"
  >
    <p v-if="historyDeleteError" class="admin-error mt-3" role="alert">
      {{ historyDeleteError }}
    </p>
  </ConfirmDialog>
</template>
