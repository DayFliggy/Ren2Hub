import { ref, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import type { Observability } from '@/api/observability'
import { opsMonitorApi } from '@/api/opsMonitor'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from './useLatestRequest'

/** The Root resource snapshot never blocks the wallet or usage summaries. */
export function useDashboardResources() {
  const auth = useAuthStore()
  const { t } = useI18n()
  const systemRequest = useLatestRequest()
  const system = ref<Observability | null>(null)
  const systemLoading = ref(false)
  const systemError = ref<string | null>(null)

  const errorMessage = (error: unknown) =>
    error instanceof Error ? error.message : t('common.failed')

  async function loadSystem() {
    if (!auth.isRoot) return
    systemLoading.value = true
    systemError.value = null
    const result = await systemRequest.run(
      async (signal) => (await opsMonitorApi.stats(signal)).observability
    )
    if (result.stale) return
    systemLoading.value = false
    if (!result.ok) {
      system.value = null
      systemError.value = errorMessage(result.error)
      return
    }
    system.value = result.value
  }

  // One backend snapshot supplies each refresh; the browser never samples or
  // synthesizes its own resource history.
  const polling = useIntervalFn(
    () => {
      if (!systemLoading.value) void loadSystem()
    },
    10_000,
    { immediate: false }
  )

  watch(
    () => [auth.user?.id, auth.user?.role],
    () => {
      systemRequest.cancel()
      polling.pause()
      system.value = null
      systemError.value = null
      systemLoading.value = false
      if (auth.isRoot) {
        void loadSystem()
        polling.resume()
      }
    },
    { immediate: true }
  )

  return {
    system,
    systemLoading,
    systemError,
    loadSystem,
  }
}
