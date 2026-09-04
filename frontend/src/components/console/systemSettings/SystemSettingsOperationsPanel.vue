<script setup lang="ts">
import { onMounted, ref } from 'vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { api } from '@/api/console'
import { useToast } from '@/composables/useToast'

const toast = useToast()
const loading = ref(false)
const syncing = ref(false)
const confirming = ref(false)
const instances = ref<Array<Record<string, unknown>>>([])
const tasks = ref<Array<Record<string, unknown>>>([])
const pricingStatus = ref<Record<string, unknown> | null>(null)
const currentTask = ref<Record<string, unknown> | null>(null)

async function refresh() {
  loading.value = true
  try {
    const [pricing, taskList, task, nodes] = await Promise.all([
      api.get<Record<string, unknown>>('/api/auto_pricing/status'),
      api.get<Array<Record<string, unknown>>>('/api/system-task/list', {
        limit: 20,
      }),
      api.get<Record<string, unknown> | null>('/api/system-task/current', {
        type: 'log_cleanup',
      }),
      api.get<Array<Record<string, unknown>>>('/api/system-info/instances'),
    ])
    pricingStatus.value = pricing
    tasks.value = taskList ?? []
    currentTask.value = task
    instances.value = nodes ?? []
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loading.value = false
  }
}

async function syncPricing() {
  syncing.value = true
  try {
    pricingStatus.value = await api.post('/api/auto_pricing/sync')
    toast.success('自动定价已同步')
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    syncing.value = false
  }
}

async function syncRatios() {
  try {
    await api.post('/api/ratio_sync/fetch', { channel_ids: [] })
    toast.success('倍率同步任务已提交')
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  }
}

async function cleanupInstances() {
  try {
    await api.delete('/api/system-info/stale-instances')
    toast.success('过期实例已清理')
    await refresh()
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  }
}

function confirmCleanup() {
  confirming.value = false
  void cleanupInstances()
}

async function createCleanupTask() {
  try {
    const target = Math.floor(Date.now() / 1000) - 30 * 86400
    await api.post(`/api/system-task/log-cleanup?target_timestamp=${target}`)
    toast.success('日志清理任务已创建')
    await refresh()
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  }
}

onMounted(refresh)
</script>

<template>
  <section class="operations-panel">
    <div class="operations-toolbar">
      <ConsoleButton
        variant="ghost"
        size="sm"
        :loading="loading"
        @click="refresh"
        >刷新状态</ConsoleButton
      >
      <ConsoleButton
        variant="secondary"
        size="sm"
        :loading="syncing"
        @click="syncPricing"
        >立即同步自动定价</ConsoleButton
      >
      <ConsoleButton variant="secondary" size="sm" @click="syncRatios"
        >同步上游倍率</ConsoleButton
      >
      <ConsoleButton variant="secondary" size="sm" @click="createCleanupTask"
        >创建日志清理任务</ConsoleButton
      >
      <ConsoleButton variant="danger" size="sm" @click="confirming = true"
        >清理过期实例</ConsoleButton
      >
    </div>
    <dl class="operations-summary">
      <div>
        <dt>自动定价</dt>
        <dd>{{ pricingStatus?.status ?? pricingStatus?.state ?? '—' }}</dd>
      </div>
      <div>
        <dt>当前任务</dt>
        <dd>{{ currentTask ? (currentTask.status ?? '运行中') : '无' }}</dd>
      </div>
      <div>
        <dt>任务记录</dt>
        <dd>{{ tasks.length }}</dd>
      </div>
      <div>
        <dt>实例数</dt>
        <dd>{{ instances.length }}</dd>
      </div>
    </dl>
  </section>
  <ConfirmDialog
    :open="confirming"
    title="清理过期实例"
    message="仅删除已过期且不再上报心跳的实例。"
    confirm-text="确认清理"
    @cancel="confirming = false"
    @confirm="confirmCleanup"
  />
</template>

<style scoped>
.operations-panel {
  margin-top: 1.5rem;
  border-top: 1px dashed var(--border-default);
  padding-top: 1rem;
}
.operations-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.operations-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.75rem;
  margin-top: 1rem;
}
.operations-summary dt {
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}
.operations-summary dd {
  margin-top: 0.125rem;
  font-weight: 700;
  color: var(--text-primary);
  overflow-wrap: anywhere;
}
@media (max-width: 767px) {
  .operations-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
