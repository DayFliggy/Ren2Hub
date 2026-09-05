<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RefreshCw, Trash2 } from 'lucide-vue-next'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import {
  systemManagementApi,
  type SystemInstance,
  type SystemTask,
} from '@/api/adminManagement'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { adminManagementMessages } from '@/i18n/adminManagement'
import { useI18n } from 'vue-i18n'
import '@/components/admin/admin.css'
const { t, locale } = useI18n({
  useScope: 'local',
  messages: adminManagementMessages,
})
const request = useAdminRequest()
const instances = ref<SystemInstance[]>([])
const tasks = ref<SystemTask[]>([])
const cleanup = ref<'instances' | 'logs' | null>(null)
const before = ref('')
const date = (stamp: number) =>
  new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(stamp * 1000))
async function load() {
  await request.run(async (signal) => {
    ;[instances.value, tasks.value] = await Promise.all([
      systemManagementApi.instances(signal),
      systemManagementApi.tasks(50, signal),
    ])
  })
}
async function removeStale() {
  await request.run(async () => {
    await systemManagementApi.deleteStale()
    cleanup.value = null
    await load()
  })
}
async function removeNode(name: string) {
  await request.run(async () => {
    await systemManagementApi.deleteStale(name)
    await load()
  })
}
async function cleanLogs() {
  const stamp = Date.parse(before.value)
  if (!Number.isFinite(stamp) || stamp >= Date.now()) return
  await request.run(async () => {
    await systemManagementApi.cleanup(Math.floor(stamp / 1000))
    cleanup.value = null
    await load()
  })
}
onMounted(load)
</script>
<template>
  <div class="admin-page">
    <div class="admin-toolbar">
      <h1>{{ t('system') }}</h1>
      <ConsoleButton
        variant="ghost"
        :loading="request.loading.value"
        @click="load"
        ><RefreshCw :size="16" />{{ t('refresh') }}</ConsoleButton
      ><ConsoleButton variant="danger" @click="cleanup = 'instances'"
        ><Trash2 :size="16" />{{ t('cleanupStale') }}</ConsoleButton
      ><ConsoleButton variant="secondary" @click="cleanup = 'logs'">{{
        t('cleanupLogs')
      }}</ConsoleButton>
    </div>
    <p v-if="request.error" class="admin-error" role="alert">
      {{ request.error }}
    </p>
    <ConsoleCard :title="t('nodes')" :padded="false"
      ><div class="admin-table-scroll">
        <table class="admin-table">
          <thead>
            <tr>
              <th>{{ t('name') }}</th>
              <th>{{ t('status') }}</th>
              <th>{{ t('version') }}</th>
              <th>{{ t('hostname') }}</th>
              <th>{{ t('cpu') }}</th>
              <th>{{ t('memory') }}</th>
              <th>{{ t('storage') }}</th>
              <th>{{ t('lastSeen') }}</th>
              <th>{{ t('actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="node in instances" :key="node.node_name">
              <td>
                {{ node.node_name }}
                <span v-if="node.master" class="admin-muted"
                  >({{ t('master') }})</span
                >
              </td>
              <td>{{ t(node.status) }}</td>
              <td>{{ node.version || '—' }}</td>
              <td>{{ node.hostname || '—' }}</td>
              <td>{{ node.cpu === null ? '—' : `${node.cpu}%` }}</td>
              <td>{{ node.memory === null ? '—' : `${node.memory}%` }}</td>
              <td>{{ node.storage === null ? '—' : `${node.storage}%` }}</td>
              <td>{{ date(node.last_seen_at) }}</td>
              <td>
                <IconButton
                  v-if="node.status === 'stale'"
                  :label="t('remove')"
                  tone="danger"
                  @click="removeNode(node.node_name)"
                  ><Trash2 :size="15"
                /></IconButton>
              </td>
            </tr>
            <tr v-if="!instances.length">
              <td colspan="9" class="admin-muted">{{ t('empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div></ConsoleCard
    >
    <ConsoleCard :title="t('tasks')" :padded="false"
      ><div class="admin-table-scroll">
        <table class="admin-table">
          <thead>
            <tr>
              <th>{{ t('type') }}</th>
              <th>{{ t('status') }}</th>
              <th>{{ t('progress') }}</th>
              <th>{{ t('processed') }}</th>
              <th>{{ t('total') }}</th>
              <th>{{ t('owner') }}</th>
              <th>{{ t('createdAt') }}</th>
              <th>{{ t('error') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="task in tasks" :key="task.task_id">
              <td>{{ t(task.type) }}</td>
              <td>{{ t(task.status) }}</td>
              <td>{{ task.progress === null ? '—' : `${task.progress}%` }}</td>
              <td>{{ task.processed ?? '—' }}</td>
              <td>{{ task.total ?? '—' }}</td>
              <td>{{ task.locked_by || '—' }}</td>
              <td>{{ date(task.created_at) }}</td>
              <td class="admin-error">{{ task.error }}</td>
            </tr>
            <tr v-if="!tasks.length">
              <td colspan="8" class="admin-muted">{{ t('empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div></ConsoleCard
    >
    <ConfirmDialog
      v-if="cleanup === 'instances'"
      :open="true"
      :title="t('cleanupStale')"
      :message="t('cleanupStaleMessage')"
      :confirm-text="t('remove')"
      :loading="request.loading.value"
      @confirm="removeStale"
      @cancel="cleanup = null"
    /><ConsoleCard v-if="cleanup === 'logs'" :title="t('cleanupLogs')"
      ><form class="admin-form" @submit.prevent="cleanLogs">
        <label class="admin-field"
          ><span>{{ t('cleanupBefore') }}</span
          ><input v-model="before" type="datetime-local" required
        /></label>
        <p class="admin-muted">
          {{ t('cleanupMessage', { date: before || '—' }) }}
        </p>
        <div class="admin-row-actions">
          <ConsoleButton type="submit" :loading="request.loading.value">{{
            t('cleanupLogs')
          }}</ConsoleButton
          ><ConsoleButton
            type="button"
            variant="ghost"
            @click="cleanup = null"
            >{{ t('cancel') }}</ConsoleButton
          >
        </div>
      </form></ConsoleCard
    >
  </div>
</template>
