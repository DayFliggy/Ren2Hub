import { ref, watch } from 'vue'
import { useDocumentVisibility, useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import { systemStatusApi, type SystemStatus } from '@/api/systemStatus'
import { ApiError } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from './useLatestRequest'

/** The shared system snapshot never blocks the wallet or usage summaries. */
export function useDashboardResources() {
  const auth = useAuthStore()
  const { t } = useI18n()
  const visibility = useDocumentVisibility()
  const systemRequest = useLatestRequest()
  const system = ref<SystemStatus | null>(null)
  const systemLoading = ref(false)
  const systemError = ref<string | null>(null)

  const errorMessage = (error: unknown) => {
    if (error instanceof ApiError && error.status === 429)
      return t('dashboard.systemStatus.rateLimited')
    return error instanceof Error ? error.message : t('common.failed')
  }

  async function loadSystem() {
    if (!auth.isAuthenticated) return
    systemLoading.value = true
    systemError.value = null
    const result = await systemRequest.run((signal) =>
      systemStatusApi.get(signal)
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

  // A hidden tab stops polling; returning refreshes at once so the card does
  // not show a reading from before the tab was hidden.
  function syncPolling() {
    if (!auth.isAuthenticated || visibility.value === 'hidden') {
      polling.pause()
      return
    }
    if (!systemLoading.value) void loadSystem()
    polling.resume()
  }

  watch(visibility, syncPolling)

  watch(
    () => auth.user?.id,
    () => {
      systemRequest.cancel()
      polling.pause()
      system.value = null
      systemError.value = null
      systemLoading.value = false
      syncPolling()
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
