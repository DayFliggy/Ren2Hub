import { ref, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import { getRequestLimits, type RequestLimits } from '@/api/dashboardResources'
import type { Observability } from '@/api/observability'
import { opsMonitorApi } from '@/api/opsMonitor'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from './useLatestRequest'

/** Supplemental reads never block the wallet or usage summaries. */
export function useDashboardResources() {
  const auth = useAuthStore()
  const { t } = useI18n()
  const limitsRequest = useLatestRequest()
  const systemRequest = useLatestRequest()
  const limits = ref<RequestLimits | null>(null)
  const limitsLoading = ref(false)
  const limitsError = ref<string | null>(null)
  const system = ref<Observability | null>(null)
  const systemLoading = ref(false)
  const systemError = ref<string | null>(null)

  const errorMessage = (error: unknown) =>
    error instanceof Error ? error.message : t('common.failed')

  async function loadLimits() {
    if (!auth.user) return
    limitsLoading.value = true
    limitsError.value = null
    limits.value = null
    const result = await limitsRequest.run(getRequestLimits)
    if (result.stale) return
    limitsLoading.value = false
    if (!result.ok) {
      limitsError.value = errorMessage(result.error)
      return
    }
    limits.value = result.value
  }

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
      limitsRequest.cancel()
      systemRequest.cancel()
      polling.pause()
      limits.value = null
      system.value = null
      limitsError.value = null
      systemError.value = null
      limitsLoading.value = false
      systemLoading.value = false
      if (auth.user) void loadLimits()
      if (auth.isRoot) {
        void loadSystem()
        polling.resume()
      }
    },
    { immediate: true }
  )

  return {
    limits,
    limitsLoading,
    limitsError,
    loadLimits,
    system,
    systemLoading,
    systemError,
    loadSystem,
  }
}
