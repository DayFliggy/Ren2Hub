<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { getAuthSessionGeneration } from '@/api/authSession'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
type BalanceTask = {
  task_id: string
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  state?: { processed?: number; total?: number; progress?: number }
  error?: string
}
const emit = defineEmits<{ complete: [] }>()
const { t } = useI18n()
const task = ref<BalanceTask | null>(null)
const busy = ref(false)
const polling = ref(false)
const error = ref('')
const request = new AbortController()
const session = getAuthSessionGeneration()
let timer: ReturnType<typeof setTimeout> | undefined
const active = computed(
  () => task.value?.status === 'pending' || task.value?.status === 'running'
)
const current = () =>
  !request.signal.aborted && session === getAuthSessionGeneration()
function receive(next: BalanceTask) {
  if (!current()) return
  if (
    !next.task_id ||
    !['pending', 'running', 'succeeded', 'failed'].includes(next.status)
  )
    throw new Error(t('pluginExtensions.error'))
  task.value = next
  if (next.status === 'failed')
    error.value = next.error || t('pluginExtensions.error')
  else if (next.status === 'succeeded') emit('complete')
  else timer = setTimeout(() => void poll(), 2000)
}
async function poll() {
  if (!current() || !task.value || polling.value) return
  clearTimeout(timer)
  polling.value = true
  error.value = ''
  try {
    receive(
      await api.get<BalanceTask>(
        `/api/channel/update_balance/task/${encodeURIComponent(task.value.task_id)}`,
        undefined,
        { signal: request.signal }
      )
    )
  } catch (cause) {
    if (current())
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    polling.value = false
  }
}
async function start() {
  if (busy.value || active.value || !current()) return
  busy.value = true
  error.value = ''
  task.value = null
  try {
    receive(
      await api.get<BalanceTask>('/api/channel/update_balance', undefined, {
        signal: request.signal,
      })
    )
  } catch (cause) {
    if (current())
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (current()) busy.value = false
  }
}
onBeforeUnmount(() => {
  clearTimeout(timer)
  request.abort()
})
</script>
<template>
  <div class="flex flex-wrap items-center gap-2 text-xs">
    <ConsoleButton
      size="sm"
      variant="secondary"
      :disabled="busy || active"
      @click="start"
      >{{ t('pluginExtensions.balanceAll') }}</ConsoleButton
    >
    <span v-if="active" role="status">{{
      task?.state
        ? t('pluginExtensions.balanceProgress', {
            completed: task.state.processed ?? 0,
            total: task.state.total ?? 0,
          })
        : t('pluginExtensions.balancePending')
    }}</span>
    <span v-if="task?.status === 'succeeded'" role="status">{{
      t('pluginExtensions.balanceDone')
    }}</span>
    <span v-if="error" role="alert" class="text-[var(--status-danger)]">{{
      error
    }}</span>
    <ConsoleButton
      v-if="error && active"
      :disabled="polling"
      size="sm"
      variant="secondary"
      @click="poll"
      >{{ t('common.retry') }}</ConsoleButton
    >
  </div>
</template>
