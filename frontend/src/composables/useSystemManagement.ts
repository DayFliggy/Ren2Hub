import { computed, ref, watch } from 'vue'
import {
  isActiveSystemTask,
  systemManagementApi,
  type SystemTask,
} from '@/api/systemManagement'
import { usePolledFeed } from './usePolledFeed'

const SUMMARY_INTERVAL_MS = 60_000
const ACTIVE_INTERVAL_MS = 8000

/** Root-only system-task state for the ops monitor. */
export function useSystemManagement() {
  const summary = usePolledFeed(
    systemManagementApi.summary,
    () => SUMMARY_INTERVAL_MS
  )
  const active = usePolledFeed(
    async (signal) =>
      (await systemManagementApi.tasks({ scope: 'active', limit: 100 }, signal))
        .items,
    (rows: SystemTask[] | null) =>
      rows?.some(isActiveSystemTask) ? ACTIVE_INTERVAL_MS : null
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
    () => null
  )
  const activeTasks = computed(() =>
    (active.data.value ?? []).filter(isActiveSystemTask)
  )
  const historyTasks = computed(() =>
    (history.data.value ?? []).filter((row) => !isActiveSystemTask(row))
  )
  /** Registered handler types drive the history filter. */
  const taskTypes = computed(() =>
    (summary.data.value ?? []).map((row) => row.type)
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
  // Scheduled runs start on the server; the summary reveals them, so restart
  // active polling when it reports work the active list has not seen yet.
  watch(summary.data, (types) => {
    if (
      types?.some((type) => type.active) &&
      !activeTasks.value.length &&
      !active.loading.value
    )
      void active.refresh()
  })
  return {
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
  }
}
