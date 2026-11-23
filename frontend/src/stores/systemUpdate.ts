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
  let pendingForce = false
  let request: AbortController | null = null

  /** Concurrent callers of the same kind share the read in flight. A forced
   *  check replaces an unforced read still running, so the refresh reaches
   *  GitHub and the older answer is dropped. */
  function check(force: boolean): Promise<void> {
    if (pending && (pendingForce || !force)) return pending
    if (!pending && !force && info.value) return Promise.resolve()
    request?.abort()
    const current = new AbortController()
    request = current
    pendingForce = force
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

  // A different account or a lost Root role ends the reads of the old one.
  // Synchronous, so the cache is empty before the top bar mounts the badge
  // for the new account and that badge starts its own check.
  watch(() => (auth.isRoot ? auth.user?.id : null), clear, { flush: 'sync' })

  return { info, loading, error, check, clear }
})
