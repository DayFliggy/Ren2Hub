import { computed, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { getAuthSessionGeneration } from '@/api/authSession'
import { api } from '@/api/client'

export type ChannelBalanceTaskState = {
  task_id: string
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  state?: { processed?: number; total?: number; progress?: number }
  error?: string
}

export function useChannelBalanceTask(options?: { onComplete?: () => void }) {
  const { t } = useI18n()
  const task = ref<ChannelBalanceTaskState | null>(null)
  const busy = ref(false)
  const polling = ref(false)
  const error = ref('')
  let request = new AbortController()
  let runGeneration = 0
  let disposed = false
  const session = getAuthSessionGeneration()
  let timer: ReturnType<typeof setTimeout> | undefined

  const active = computed(
    () => task.value?.status === 'pending' || task.value?.status === 'running'
  )

  const current = (generation = runGeneration) =>
    !disposed &&
    !request.signal.aborted &&
    generation === runGeneration &&
    session === getAuthSessionGeneration()

  function receive(next: ChannelBalanceTaskState, generation: number) {
    if (!current(generation)) return
    if (
      !next ||
      typeof next !== 'object' ||
      !next.task_id ||
      !['pending', 'running', 'succeeded', 'failed'].includes(next.status)
    )
      throw new Error(t('pluginExtensions.error'))
    task.value = next
    if (next.status === 'failed') {
      error.value = next.error || t('pluginExtensions.error')
    } else if (next.status === 'succeeded') {
      options?.onComplete?.()
    } else {
      timer = setTimeout(() => void poll(), 2000)
    }
  }

  async function poll() {
    const generation = runGeneration
    if (!current(generation) || !task.value || polling.value) return
    clearTimeout(timer)
    polling.value = true
    error.value = ''
    try {
      receive(
        await api.get<ChannelBalanceTaskState>(
          `/api/channel/update_balance/task/${encodeURIComponent(task.value.task_id)}`,
          undefined,
          { signal: request.signal }
        ),
        generation
      )
    } catch (cause) {
      if (current(generation))
        error.value = cause instanceof Error ? cause.message : String(cause)
    } finally {
      polling.value = false
    }
  }

  async function start() {
    const generation = runGeneration
    if (busy.value || active.value || !current(generation)) return
    busy.value = true
    error.value = ''
    task.value = null
    try {
      receive(
        await api.get<ChannelBalanceTaskState>(
          '/api/channel/update_balance',
          undefined,
          {
            signal: request.signal,
          }
        ),
        generation
      )
    } catch (cause) {
      if (current(generation))
        error.value = cause instanceof Error ? cause.message : String(cause)
    } finally {
      busy.value = false
    }
  }

  function dismiss() {
    if (active.value && !error.value) return
    clearTimeout(timer)
    task.value = null
    error.value = ''
  }

  function cancel() {
    runGeneration += 1
    clearTimeout(timer)
    request.abort()
    if (!disposed) request = new AbortController()
    task.value = null
    error.value = ''
    busy.value = false
    polling.value = false
  }

  onScopeDispose(() => {
    disposed = true
    clearTimeout(timer)
    request.abort()
  })

  return {
    task,
    busy,
    polling,
    active,
    error,
    start,
    poll,
    dismiss,
    cancel,
  }
}
