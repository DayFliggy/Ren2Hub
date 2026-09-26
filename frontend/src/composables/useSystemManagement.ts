import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from 'vue'
import {
  isActiveSystemTask,
  systemManagementApi,
  type SystemTask,
} from '@/api/systemManagement'

// Each section owns its request and timer so a failing feed never blocks its peers.
function useSystemFeed<T>(
  fetch: (signal: AbortSignal) => Promise<T[]>,
  interval: (rows: T[]) => number | null
) {
  const rows = shallowRef<T[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
  const updatedAt = ref(0)
  let controller: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let mounted = false

  function stop() {
    clearTimeout(timer)
    timer = undefined
    controller?.abort()
    controller = undefined
    loading.value = false
  }

  async function refresh() {
    stop()
    if (!mounted || document.hidden) return
    const request = new AbortController()
    controller = request
    loading.value = true
    error.value = ''
    try {
      const data = await fetch(request.signal)
      if (request.signal.aborted || controller !== request) return
      rows.value = data
      loaded.value = true
      updatedAt.value = Date.now()
    } catch (cause) {
      if (!request.signal.aborted && controller === request)
        error.value = cause instanceof Error ? cause.message : String(cause)
    } finally {
      if (controller === request) {
        loading.value = false
        controller = undefined
        const delay = interval(rows.value)
        if (mounted && !document.hidden && delay !== null)
          timer = setTimeout(() => void refresh(), delay)
      }
    }
  }

  function onVisibilityChange() {
    if (document.hidden) stop()
    else void refresh()
  }

  onMounted(() => {
    mounted = true
    document.addEventListener('visibilitychange', onVisibilityChange)
    void refresh()
  })
  onBeforeUnmount(() => {
    mounted = false
    document.removeEventListener('visibilitychange', onVisibilityChange)
    stop()
  })

  return { rows, loading, loaded, error, updatedAt, refresh }
}

export function useSystemManagement() {
  const instances = useSystemFeed(systemManagementApi.instances, () => 30_000)
  const taskInterval = (rows: SystemTask[]) =>
    rows.some(isActiveSystemTask) ? 8000 : null
  const active = useSystemFeed(
    async (signal) =>
      (await systemManagementApi.tasks({ scope: 'active', limit: 100 }, signal))
        .items,
    taskInterval
  )
  const historyType = ref('')
  const historyStatus = ref<'' | 'succeeded' | 'failed'>('')
  const historyPage = ref(1)
  const historyPageSize = ref(20)
  const historyTotal = ref(0)
  const history = useSystemFeed(
    async (signal) => {
      const page = await systemManagementApi.tasks(
        {
          scope: 'history',
          type: historyType.value,
          status: historyStatus.value,
          offset: (historyPage.value - 1) * historyPageSize.value,
          limit: historyPageSize.value,
        },
        signal
      )
      if (!signal.aborted) historyTotal.value = page.total
      return page.items
    },
    () => null
  )
  watch([historyType, historyStatus, historyPageSize], () => {
    if (historyPage.value !== 1) historyPage.value = 1
    else void history.refresh()
  })
  watch(historyPage, () => void history.refresh())
  watch(active.rows, (_rows, previous) => {
    if (previous.some(isActiveSystemTask) && !history.loading.value)
      void history.refresh()
  })
  const activeTasks = computed(() =>
    active.rows.value.filter(isActiveSystemTask)
  )
  const historyTasks = computed(() =>
    history.rows.value.filter((row) => !isActiveSystemTask(row))
  )
  const staleCount = computed(
    () => instances.rows.value.filter((row) => row.status === 'stale').length
  )
  return {
    instances,
    active,
    history,
    activeTasks,
    historyTasks,
    staleCount,
    historyType,
    historyStatus,
    historyPage,
    historyPageSize,
    historyTotal,
  }
}

export function useSystemTask() {
  const taskId = ref('')
  const feed = useSystemFeed<SystemTask>(
    async (signal) =>
      taskId.value
        ? [await systemManagementApi.task(taskId.value, signal)]
        : [],
    (rows) => (rows.some(isActiveSystemTask) ? 8000 : null)
  )
  const task = computed(() => feed.rows.value[0] ?? null)
  function follow(value: SystemTask) {
    taskId.value = value.task_id
    feed.rows.value = [value]
    void feed.refresh()
  }
  return { task, error: feed.error, refresh: feed.refresh, follow }
}
