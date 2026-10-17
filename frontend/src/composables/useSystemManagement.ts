import { computed, ref, watch } from 'vue'
import {
  isActiveSystemTask,
  systemManagementApi,
  type SystemTask,
} from '@/api/systemManagement'
import { usePolledFeed } from './usePolledFeed'

const IDLE_INTERVAL_MS = 60_000
const ACTIVE_INTERVAL_MS = 8000

/** Root-only system-task state for the ops monitor. */
export function useSystemManagement() {
  const active = usePolledFeed(
    async (signal) =>
      (await systemManagementApi.tasks({ scope: 'active', limit: 100 }, signal))
        .items,
    (rows: SystemTask[] | null) =>
      rows?.some(isActiveSystemTask) ? ACTIVE_INTERVAL_MS : IDLE_INTERVAL_MS
  )
  const historyType = ref('')
  const historyStatus = ref<'' | 'succeeded' | 'failed'>('')
  const historyPage = ref(1)
  const historyPageSize = ref(20)
  const historyTotal = ref(0)
  const history = usePolledFeed(
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
    () => IDLE_INTERVAL_MS
  )
  const activeTasks = computed(() =>
    (active.data.value ?? []).filter(isActiveSystemTask)
  )
  const historyTasks = computed(() =>
    (history.data.value ?? []).filter((row) => !isActiveSystemTask(row))
  )
  // Suggestions come from loaded records; the type filter also accepts text.
  const taskTypes = computed(() =>
    [
      ...new Set(
        [...(active.data.value ?? []), ...(history.data.value ?? [])].map(
          (row) => row.type
        )
      ),
    ].sort()
  )
  watch([historyType, historyStatus, historyPageSize], () => {
    if (historyPage.value !== 1) historyPage.value = 1
    else void history.refresh()
  })
  watch(historyPage, () => void history.refresh())
  watch(active.data, (_rows, previous) => {
    if (previous?.some(isActiveSystemTask) && !history.loading.value)
      void history.refresh()
  })
  return {
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
  }
}
