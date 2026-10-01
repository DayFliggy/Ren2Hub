import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  type ComputedRef,
  type Ref,
  type ShallowRef,
} from 'vue'

export interface PolledFeed<T> {
  /** Last successful result; `null` until the first success. */
  data: ShallowRef<T | null>
  loading: Ref<boolean>
  /** Message of the latest failure; cleared by the next success. */
  error: Ref<string>
  /** `Date.now()` of the last success, 0 before it. */
  updatedAt: Ref<number>
  /** Previously loaded data is shown while the latest refresh failed. */
  stale: ComputedRef<boolean>
  refresh: () => Promise<void>
}

/**
 * One independently polled section. Each feed owns its request and timer, so
 * a failing feed never blocks its peers. A refresh aborts the request it
 * supersedes, polling pauses while the document is hidden, and unmounting
 * aborts the request and clears the timer. `interval` sees the latest data
 * (or `null`) and returns the next delay in ms, or `null` to stop polling.
 */
export function usePolledFeed<T>(
  fetch: (signal: AbortSignal) => Promise<T>,
  interval: (data: T | null) => number | null
): PolledFeed<T> {
  const data = shallowRef<T | null>(null)
  const loading = ref(false)
  const error = ref('')
  const updatedAt = ref(0)
  let controller: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let mounted = false

  function stop() {
    clearTimeout(timer)
    timer = undefined
    controller?.abort()
    controller = undefined
    loading.value = false
  }

  async function refresh() {
    stop()
    if (!mounted || document.hidden) return
    const request = new AbortController()
    controller = request
    loading.value = true
    try {
      const value = await fetch(request.signal)
      if (controller !== request) return
      data.value = value
      error.value = ''
      updatedAt.value = Date.now()
    } catch (cause) {
      if (controller === request)
        error.value = cause instanceof Error ? cause.message : String(cause)
    } finally {
      if (controller === request) {
        loading.value = false
        controller = undefined
        const delay = interval(data.value)
        if (mounted && !document.hidden && delay !== null)
          timer = setTimeout(() => void refresh(), delay)
      }
    }
  }

  function onVisibilityChange() {
    if (document.hidden) stop()
    else void refresh()
  }

  onMounted(() => {
    mounted = true
    document.addEventListener('visibilitychange', onVisibilityChange)
    void refresh()
  })
  onBeforeUnmount(() => {
    mounted = false
    document.removeEventListener('visibilitychange', onVisibilityChange)
    stop()
  })

  return {
    data,
    loading,
    error,
    updatedAt,
    stale: computed(() => Boolean(error.value) && data.value !== null),
    refresh,
  }
}
