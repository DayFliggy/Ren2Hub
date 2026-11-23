import { ref, watch } from 'vue'
import { defineStore } from 'pinia'

import { systemUpdateApi, type UpdateCheck } from '@/api/systemUpdate'

import { useAuthStore } from './auth'

/**
 * The release check behind the Root version badge, after Sub2API's
 * fetchVersion/clearVersionCache. The server caches GitHub for 20 minutes and
 * reports a `warning` when it falls back to that cache, so the page reads once
 * and only refreshes on request; there is no polling.
 */
export const useSystemUpdateStore = defineStore('system-update', () => {
  const auth = useAuthStore()
  const info = ref<UpdateCheck | null>(null)
  const loading = ref(false)
  const error = ref<unknown>(null)
  let pending: Promise<void> | null = null
  let request: AbortController | null = null

  /** Concurrent callers share the request in flight, forced or not. */
  function check(force: boolean): Promise<void> {
    if (pending) return pending
    if (!force && info.value) return Promise.resolve()
    const current = new AbortController()
    request = current
    loading.value = true
    pending = read(force, current.signal).finally(() => {
      if (request !== current) return
      request = null
      pending = null
      loading.value = false
    })
    return pending
  }

  async function read(force: boolean, signal: AbortSignal) {
    try {
      const value = await systemUpdateApi.checkUpdates(force, signal)
      if (signal.aborted) return
      info.value = value
      error.value = null
    } catch (cause) {
      if (!signal.aborted) error.value = cause
    }
  }

  /** Forgets the result after an update or rollback replaced the program. */
  function clear() {
    request?.abort()
    request = null
    pending = null
    loading.value = false
    info.value = null
    error.value = null
  }

  watch(() => auth.user?.id, clear)

  return { info, loading, error, check, clear }
})
