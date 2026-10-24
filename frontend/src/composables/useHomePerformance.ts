import { computed, onScopeDispose, ref, watch } from 'vue'
import { getPerformanceSummary } from '@/api/modelMarket'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from './useLatestRequest'

export function useHomePerformance() {
  const app = useAppStore()
  const auth = useAuthStore()
  const request = useLatestRequest()
  const latency = ref<number | null>(null)
  const state = ref<
    'loading' | 'notPublic' | 'signIn' | 'empty' | 'error' | 'ready'
  >('loading')
  const error = ref<unknown>(null)
  watch(
    [
      () => app.statusReachable,
      () => app.status.HeaderNavModules,
      () => auth.user,
      () => auth.checked,
      () => !app.statusReachable && app.phase === 'error',
    ],
    async () => {
      request.cancel()
      latency.value = null
      error.value = null
      const access = app.moduleAccess('pricing')
      if (!app.statusReachable) {
        state.value = app.phase === 'error' ? 'error' : 'loading'
        return
      }
      if (!access.enabled) {
        state.value = 'notPublic'
        return
      }
      if (!auth.checked) {
        state.value = 'loading'
        return
      }
      if (access.requireAuth && !auth.isAuthenticated) {
        state.value = 'signIn'
        return
      }
      state.value = 'loading'
      const result = await request.run((signal) =>
        getPerformanceSummary(auth.isAuthenticated, signal)
      )
      if (result.stale) return
      if (!result.ok) {
        error.value = result.error
        state.value = 'error'
        return
      }
      latency.value = result.value.summary?.avg_latency_ms ?? null
      state.value = latency.value === null ? 'empty' : 'ready'
    },
    { immediate: true, flush: 'sync' }
  )
  onScopeDispose(() => {
    latency.value = null
    error.value = null
  })
  return { latency: computed(() => latency.value), state, error }
}
