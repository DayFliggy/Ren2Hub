import { onScopeDispose } from 'vue'
import { useRouter, type RouteLocationNormalized } from 'vue-router'

/** Protect a mounted editor's draft on navigation and browser unload. */
export function useUnsavedChanges(options: {
  dirty: () => boolean
  message: () => string
  busy: () => boolean
  leaves?: (
    to: RouteLocationNormalized,
    from: RouteLocationNormalized
  ) => boolean
}) {
  const router = useRouter()
  const removeGuard = router.beforeEach((to, from) => {
    const leaves = options.leaves?.(to, from) ?? to.path !== from.path
    if (!leaves) return true
    if (options.busy()) return false
    return !options.dirty() || window.confirm(options.message())
  })
  function beforeUnload(event: BeforeUnloadEvent) {
    if (options.dirty() || options.busy()) event.preventDefault()
  }
  window.addEventListener('beforeunload', beforeUnload)
  onScopeDispose(() => {
    removeGuard()
    window.removeEventListener('beforeunload', beforeUnload)
  })
}
