<script setup lang="ts">
import { Info, ListChecks, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import type { SystemTask } from '@/api/systemManagement'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { systemManagementMessages } from '@/i18n/systemManagement'
import {
  formatSystemRelative,
  formatSystemTimestamp,
} from '@/utils/systemManagement'

defineProps<{
  title: string
  emptyText: string
  tasks: SystemTask[]
  loading: boolean
  loaded: boolean
  error: string
  updatedAt: number
}>()
const emit = defineEmits<{ refresh: []; details: [task: SystemTask] }>()
const { t, te, locale } = useI18n({
  useScope: 'local',
  messages: systemManagementMessages,
})
const tones = {
  pending: 'warning',
  running: 'info',
  succeeded: 'success',
  failed: 'danger',
} as const
</script>

<template>
  <section class="task-section" :aria-label="title" :aria-busy="loading">
    <header class="task-header">
      <h2>
        <ListChecks :size="17" aria-hidden="true" />{{ title
        }}<span class="task-count">{{ tasks.length }}</span>
      </h2>
      <div class="task-actions">
        <slot name="actions" />
        <IconButton
          :label="loading ? t('refreshing') : t('refresh')"
          :disabled="loading"
          @click="emit('refresh')"
          ><RefreshCw :size="16"
        /></IconButton>
      </div>
    </header>
    <div v-if="error" class="task-error" role="alert">
      <p>{{ error }}</p>
      <button type="button" :disabled="loading" @click="emit('refresh')">
        {{ t('retry') }}
      </button>
    </div>
    <p v-if="loading && !loaded" class="task-empty" role="status">
      {{ t('loading') }}
    </p>
    <p v-else-if="!tasks.length && !error" class="task-empty">
      {{ emptyText }}
    </p>
    <div v-if="tasks.length" class="admin-table-scroll">
      <table class="admin-table task-table">
        <thead>
          <tr>
            <th>{{ t('type') }}</th>
            <th>{{ t('status') }}</th>
            <th>{{ t('progress') }}</th>
            <th>{{ t('executor') }}</th>
            <th>{{ t('updated') }}</th>
            <th>{{ t('details') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="task in tasks" :key="task.task_id">
            <td :data-label="t('type')">
              <div class="task-name">
                <strong>{{ te(task.type) ? t(task.type) : task.type }}</strong
                ><code>{{ task.type }}</code>
              </div>
            </td>
            <td :data-label="t('status')">
              <StatusChip :tone="tones[task.status]">{{
                t(task.status)
              }}</StatusChip>
            </td>
            <td :data-label="t('progress')">
              <div class="task-progress">
                <progress
                  v-if="task.progress !== null"
                  :value="task.progress"
                  max="100"
                  :aria-label="t('progress')"
                  :class="`task-progress--${tones[task.status]}`"
                /><span>{{
                  task.progress === null ? '-' : `${task.progress}%`
                }}</span>
              </div>
            </td>
            <td :data-label="t('executor')">
              <code>{{ task.locked_by || '-' }}</code>
            </td>
            <td
              :data-label="t('updated')"
              :title="formatSystemTimestamp(task.updated_at, locale)"
            >
              {{ formatSystemRelative(task.updated_at, updatedAt, locale) }}
            </td>
            <td :data-label="t('details')">
              <div class="task-detail">
                <span
                  v-if="task.error"
                  class="admin-error task-error-summary"
                  >{{ task.error }}</span
                ><IconButton
                  :label="t('taskDetails')"
                  @click="emit('details', task)"
                  ><Info :size="16"
                /></IconButton>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.task-section {
  min-width: 0;
}
.task-header,
.task-header h2,
.task-actions {
  display: flex;
  align-items: center;
  gap: 0.625rem;
}
.task-header {
  justify-content: space-between;
  margin-bottom: 0.75rem;
  border-top: 1px solid var(--border-subtle);
  padding-top: 1rem;
}
.task-header h2 {
  font-size: 0.9375rem;
  font-weight: 600;
  min-width: 0;
}
.task-count {
  font-size: 0.75rem;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}
.task-empty {
  padding: 2rem 1rem;
  text-align: center;
  color: var(--text-secondary);
  font-size: 0.875rem;
}
.task-error {
  display: flex;
  gap: 1rem;
  align-items: center;
  justify-content: space-between;
  margin: 0.5rem 0;
  color: var(--status-danger-text);
  background: var(--status-danger-soft);
  padding: 0.75rem;
  font-size: 0.875rem;
  overflow-wrap: anywhere;
}
.task-error button {
  flex: none;
  text-decoration: underline;
}
.task-error button:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 3px;
}
.task-name {
  display: grid;
  gap: 0.25rem;
}
.task-name strong {
  font-weight: 500;
}
.task-table code {
  font-size: 0.6875rem;
  color: var(--text-secondary);
}
.task-progress {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.task-progress progress {
  width: 5rem;
  height: 0.375rem;
  border: 0;
  border-radius: var(--shape-small);
  overflow: hidden;
  background: var(--surface-muted);
}
.task-progress progress::-webkit-progress-bar {
  background: var(--surface-muted);
}
.task-progress progress::-webkit-progress-value {
  background: var(--progress-color);
}
.task-progress progress::-moz-progress-bar {
  background: var(--progress-color);
}
.task-progress--warning {
  --progress-color: var(--status-warning);
}
.task-progress--info {
  --progress-color: var(--status-info);
}
.task-progress--success {
  --progress-color: var(--status-success);
}
.task-progress--danger {
  --progress-color: var(--status-danger);
}
.task-detail {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.task-error-summary {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  max-width: 15rem;
  font-size: 0.75rem;
}
@media (max-width: 760px) {
  .task-table,
  .task-table tbody {
    display: block;
  }
  .task-table thead {
    display: none;
  }
  .task-table tr {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    padding: 0.5rem 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .task-table td,
  .task-table td:first-child {
    display: grid;
    grid-template-columns: 6rem minmax(0, 1fr);
    gap: 0.75rem;
    max-width: none;
    padding: 0.375rem 0;
    border: 0;
  }
  .task-table td::before {
    content: attr(data-label);
    color: var(--text-secondary);
    font-size: 0.75rem;
  }
}
</style>
