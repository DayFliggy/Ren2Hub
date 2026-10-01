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
  // A short run can finish between polls without ever appearing as active.
  // Completion timestamps also invalidate history in that case.
  watch(summary.data, (types, previous) => {
    if (types && previous) {
      const previousTypes = new Map(previous.map((type) => [type.type, type]))
      if (
        types.some((type) => {
          const before = previousTypes.get(type.type)
          return (
            type.last_success_at !== (before?.last_success_at ?? null) ||
            type.last_failure_at !== (before?.last_failure_at ?? null)
          )
        })
      )
        void history.refresh()
    }
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
