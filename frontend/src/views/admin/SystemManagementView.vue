<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import {
  AlertTriangle,
  HardDrive,
  MoreHorizontal,
  RefreshCw,
  ServerCog,
  Trash2,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import IconButton from '@/components/common/IconButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import SystemResourceMetric from '@/components/admin/SystemResourceMetric.vue'
import SystemTaskSection from '@/components/admin/SystemTaskSection.vue'
import {
  systemManagementApi,
  type SystemInstance,
  type SystemTask,
} from '@/api/systemManagement'
import { useSystemManagement } from '@/composables/useSystemManagement'
import { systemManagementMessages } from '@/i18n/systemManagement'
import {
  formatSystemBytes,
  formatSystemRelative,
  formatSystemTimestamp,
} from '@/utils/systemManagement'
import '@/components/admin/admin.css'

const { t, te, locale } = useI18n({
  useScope: 'local',
  messages: systemManagementMessages,
})
const { instances, active, history, activeTasks, historyTasks, staleCount } =
  useSystemManagement()
const deleteTarget = ref<SystemInstance | 'all' | null>(null)
const deleting = ref(false)
const deleteError = ref('')
const result = ref('')
const diskNode = ref<SystemInstance | null>(null)
const taskDetail = ref<SystemTask | null>(null)
const logDialog = ref(false)
const logMenu = ref<HTMLDetailsElement | null>(null)
const before = ref('')
const logTarget = ref<number | null>(null)
const creating = ref(false)
const logError = ref('')
let disposed = false
onBeforeUnmount(() => {
  disposed = true
})
const date = (stamp: number) => formatSystemTimestamp(stamp, locale.value)
const bytes = (value: number | null) => formatSystemBytes(value, locale.value)
const deleteMessage = computed(() =>
  deleteTarget.value === 'all'
    ? t('deleteAllMessage')
    : t('deleteNodeMessage', { name: deleteTarget.value?.display_name || '' })
)

function confirmDelete(node: SystemInstance | 'all') {
  deleteError.value = ''
  deleteTarget.value = node
}

async function deleteInstances() {
  if (!deleteTarget.value || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  result.value = ''
  try {
    const count = await systemManagementApi.deleteStale(
      deleteTarget.value === 'all' ? undefined : deleteTarget.value.node_name
    )
    if (disposed) return
    result.value = t('deleted', { count })
    deleteTarget.value = null
  } catch (cause) {
    if (!disposed)
      deleteError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) {
      deleting.value = false
      await instances.refresh()
    }
  }
}

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
    await Promise.all([active.refresh(), history.refresh()])
  } catch (cause) {
    if (!disposed)
      logError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) creating.value = false
  }
}
</script>

<template>
  <div class="admin-page system-page">
    <header class="admin-toolbar">
      <h1>{{ t('title') }}</h1>
    </header>
    <p v-if="result" class="system-result" role="status">{{ result }}</p>
    <section
      :aria-label="t('nodes')"
      :aria-busy="instances.loading.value"
      class="system-section"
    >
      <header class="system-header">
        <h2>
          <ServerCog :size="17" aria-hidden="true" />{{ t('nodes')
          }}<span class="system-count">{{ instances.rows.value.length }}</span>
        </h2>
        <div class="system-actions">
          <ConsoleButton
            variant="ghost"
            size="sm"
            :disabled="staleCount === 0 || deleting"
            @click="confirmDelete('all')"
            ><Trash2 :size="15" />{{ t('deleteAll')
            }}<span v-if="staleCount">({{ staleCount }})</span></ConsoleButton
          >
          <IconButton
            :label="instances.loading.value ? t('refreshing') : t('refresh')"
            :disabled="instances.loading.value"
            @click="instances.refresh"
            ><RefreshCw :size="16"
          /></IconButton>
        </div>
      </header>
      <div v-if="instances.error.value" class="system-error" role="alert">
        <p>{{ instances.error.value }}</p>
        <ConsoleButton
          variant="ghost"
          size="sm"
          :disabled="instances.loading.value"
          @click="instances.refresh"
          >{{ t('retry') }}</ConsoleButton
        >
      </div>
      <p
        v-if="instances.loading.value && !instances.loaded.value"
        class="system-empty"
        role="status"
      >
        {{ t('loading') }}
      </p>
      <p
        v-else-if="!instances.rows.value.length && !instances.error.value"
        class="system-empty"
      >
        {{ t('emptyNodes') }}
      </p>
      <div v-if="instances.rows.value.length" class="admin-table-scroll">
        <table class="admin-table instance-table">
          <thead>
            <tr>
              <th>{{ t('node') }}</th>
              <th>{{ t('status') }}</th>
              <th>{{ t('role') }}</th>
              <th>{{ t('cpu') }}</th>
              <th>{{ t('memory') }}</th>
              <th>{{ t('storage') }}</th>
              <th>{{ t('version') }}</th>
              <th>{{ t('platform') }}</th>
              <th>{{ t('started') }}</th>
              <th>{{ t('lastSeen') }}</th>
              <th>{{ t('actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="node in instances.rows.value" :key="node.node_name">
              <td :data-label="t('node')">
                <div class="node-identity">
                  <strong>{{ node.display_name }}</strong
                  ><code v-if="node.display_name !== node.node_name">{{
                    node.node_name
                  }}</code
                  ><span v-if="node.hostname" class="admin-muted">{{
                    node.hostname
                  }}</span
                  ><span class="node-source"
                    >{{ t('nameSource') }}:
                    {{
                      node.name_source === 'manual'
                        ? t('manual')
                        : node.name_source === 'hostname'
                          ? t('hostname')
                          : node.name_source || t('unknown')
                    }}</span
                  >
                  <details
                    v-if="node.should_configure_manually === true"
                    class="node-warning"
                  >
                    <summary>
                      <AlertTriangle :size="13" aria-hidden="true" />{{
                        t('nameNotConfigured')
                      }}
                    </summary>
                    <p>{{ t('nameConfigureDetail') }}</p>
                  </details>
                </div>
              </td>
              <td :data-label="t('status')">
                <StatusChip
                  :tone="node.status === 'online' ? 'success' : 'warning'"
                  >{{ t(node.status) }}</StatusChip
                >
              </td>
              <td :data-label="t('role')">
                {{
                  node.master === null
                    ? '-'
                    : t(node.master ? 'master' : 'worker')
                }}
              </td>
              <td :data-label="t('cpu')">
                <SystemResourceMetric :value="node.cpu" :label="t('cpu')" />
              </td>
              <td :data-label="t('memory')">
                <SystemResourceMetric
                  :value="node.memory"
                  :label="t('memory')"
                />
              </td>
              <td :data-label="t('storage')">
                <button
                  type="button"
                  class="storage-button"
                  :aria-label="`${t('storageDetail')}: ${node.display_name}`"
                  @click="diskNode = node"
                >
                  <SystemResourceMetric
                    :value="node.storage"
                    :label="t('storage')"
                  />
                </button>
              </td>
              <td :data-label="t('version')">
                <code>{{ node.version || '-' }}</code>
              </td>
              <td :data-label="t('platform')">
                <code>{{
                  [node.goos, node.goarch].filter(Boolean).join(' / ') || '-'
                }}</code>
              </td>
              <td :data-label="t('started')">{{ date(node.started_at) }}</td>
              <td :data-label="t('lastSeen')" :title="date(node.last_seen_at)">
                {{
                  formatSystemRelative(
                    node.last_seen_at,
                    instances.updatedAt.value,
                    locale
                  )
                }}
              </td>
              <td :data-label="t('actions')">
                <IconButton
                  v-if="node.status === 'stale'"
                  :label="t('deleteNode')"
                  tone="danger"
                  :disabled="deleting"
                  @click="confirmDelete(node)"
                  ><Trash2 :size="15" /></IconButton
                ><span v-else>-</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
    <SystemTaskSection
      :title="t('activeTasks')"
      :empty-text="t('emptyActive')"
      :tasks="activeTasks"
      :loading="active.loading.value"
      :loaded="active.loaded.value"
      :error="active.error.value"
      :updated-at="active.updatedAt.value"
      @refresh="active.refresh"
      @details="taskDetail = $event"
    >
      <template #actions
        ><details
          ref="logMenu"
          class="system-menu"
          @keydown.esc="logMenu && (logMenu.open = false)"
        >
          <summary :aria-label="t('actions')" :title="t('actions')">
            <MoreHorizontal :size="18" />
          </summary>
          <div class="system-menu-content">
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
      :loading="history.loading.value"
      :loaded="history.loaded.value"
      :error="history.error.value"
      :updated-at="history.updatedAt.value"
      @refresh="history.refresh"
      @details="taskDetail = $event"
    />

    <ConfirmDialog
      :open="deleteTarget !== null"
      :title="deleteTarget === 'all' ? t('deleteAll') : t('deleteNode')"
      :message="deleteMessage"
      :confirm-text="t('remove')"
      :loading="deleting"
      @confirm="deleteInstances"
      @cancel="deleteTarget = null"
      ><p v-if="deleteError" class="admin-error mt-3" role="alert">
        {{ deleteError }}
      </p></ConfirmDialog
    >
    <ConsoleModal
      :open="diskNode !== null"
      :title="t('storageDetail')"
      size="sm"
      @close="diskNode = null"
    >
      <template v-if="diskNode"
        ><h3 class="disk-title">
          <HardDrive :size="18" />{{ diskNode.display_name }}
        </h3>
        <dl class="admin-values">
          <div>
            <dt>{{ t('used') }}</dt>
            <dd>{{ bytes(diskNode.storage_used) }}</dd>
          </div>
          <div>
            <dt>{{ t('free') }}</dt>
            <dd>{{ bytes(diskNode.storage_free) }}</dd>
          </div>
          <div>
            <dt>{{ t('total') }}</dt>
            <dd>{{ bytes(diskNode.storage_total) }}</dd>
          </div>
        </dl></template
      >
      <template #footer
        ><ConsoleButton variant="secondary" @click="diskNode = null">{{
          t('cancel')
        }}</ConsoleButton></template
      >
    </ConsoleModal>
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
            <dd>
              {{ te(taskDetail.type) ? t(taskDetail.type) : taskDetail.type }}
            </dd>
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
            <dd>{{ taskDetail.locked_by || '-' }}</dd>
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
              {{ taskDetail.processed ?? '-' }} / {{ taskDetail.total ?? '-' }}
            </dd>
          </div>
          <div>
            <dt>{{ t('deletedLogs') }}</dt>
            <dd>{{ taskDetail.deleted_count ?? '-' }}</dd>
          </div>
        </dl>
        <div v-if="taskDetail.error" class="task-error-detail">
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
        t('cleanupMessage', { date: logTarget ? date(logTarget) : '-' })
      "
      :confirm-text="t('remove')"
      :loading="creating"
      @confirm="cleanLogs"
      @cancel="logTarget = null"
      ><p v-if="logError" class="admin-error mt-3" role="alert">
        {{ logError }}
      </p></ConfirmDialog
    >
  </div>
</template>

<style scoped>
.system-page {
  gap: 1.5rem;
}
.system-section {
  min-width: 0;
}
.system-header,
.system-header h2,
.system-actions {
  display: flex;
  align-items: center;
  gap: 0.625rem;
}
.system-header {
  justify-content: space-between;
  margin-bottom: 0.75rem;
  flex-wrap: wrap;
}
.system-header h2 {
  font-size: 0.9375rem;
  font-weight: 600;
}
.system-count {
  color: var(--text-secondary);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.system-result {
  color: var(--status-success-text);
  background: var(--status-success-soft);
  padding: 0.75rem;
  font-size: 0.875rem;
  overflow-wrap: anywhere;
}
.system-error {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  justify-content: space-between;
  color: var(--status-danger-text);
  background: var(--status-danger-soft);
  padding: 0.75rem;
  font-size: 0.875rem;
  overflow-wrap: anywhere;
}
.system-empty {
  padding: 2rem 1rem;
  text-align: center;
  color: var(--text-secondary);
  font-size: 0.875rem;
}
.instance-table {
  font-size: 0.75rem;
}
.instance-table th {
  white-space: nowrap;
}
.instance-table td:first-child {
  min-width: 12rem;
  width: 13rem;
}
.instance-table td:nth-child(2),
.instance-table td:nth-child(3),
.instance-table td:nth-child(7) {
  white-space: nowrap;
}
.node-identity {
  display: grid;
  gap: 0.25rem;
}
.node-identity strong {
  font-weight: 600;
  font-size: 0.875rem;
}
.node-source,
.instance-table code {
  font-size: 0.6875rem;
  color: var(--text-secondary);
}
.node-warning {
  color: var(--status-warning-text);
  font-size: 0.6875rem;
}
.node-warning summary {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  cursor: pointer;
}
.node-warning summary svg {
  flex: none;
}
.node-warning p {
  max-width: 18rem;
  margin-top: 0.375rem;
}
.storage-button {
  text-align: start;
  border-radius: var(--shape-small);
}
.storage-button:focus-visible,
.node-warning summary:focus-visible,
.system-menu summary:focus-visible,
.system-menu button:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 2px;
}
.disk-title {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  margin-bottom: 1rem;
  overflow-wrap: anywhere;
  font-size: 0.9375rem;
}
.disk-title svg {
  flex: none;
}
.task-error-detail {
  margin-top: 1.25rem;
  font-size: 0.875rem;
}
.task-error-detail pre {
  margin-top: 0.5rem;
}
.system-menu {
  position: relative;
}
.system-menu summary {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  cursor: pointer;
  border-radius: var(--shape-small);
  list-style: none;
}
.system-menu summary::-webkit-details-marker {
  display: none;
}
.system-menu-content {
  position: absolute;
  right: 0;
  top: calc(100% + 0.375rem);
  z-index: 20;
  min-width: 10rem;
  padding: 0.25rem;
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-small);
  box-shadow: var(--overlay-shadow);
}
.system-menu-content button {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 0.5rem;
  padding: 0.625rem;
  color: var(--status-danger-text);
  font-size: 0.8125rem;
  text-align: start;
}
.system-menu-content button:hover {
  background: var(--status-danger-soft);
}
@media (max-width: 760px) {
  .instance-table,
  .instance-table tbody {
    display: block;
  }
  .instance-table thead {
    display: none;
  }
  .instance-table tr {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    padding: 0.5rem 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .instance-table td,
  .instance-table td:first-child {
    display: grid;
    grid-template-columns: 6rem minmax(0, 1fr);
    align-items: start;
    gap: 0.75rem;
    max-width: none;
    min-width: 0;
    padding: 0.375rem 0;
    border: 0;
  }
  .instance-table td::before {
    content: attr(data-label);
    color: var(--text-secondary);
  }
  .instance-table td > * {
    justify-self: start;
  }
}
</style>
