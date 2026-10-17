import { computed, getCurrentScope, onScopeDispose, ref, type Ref } from 'vue'
import type { HomeRuntime } from '@/types/homeShowcase'
export function calculateRuntime(
  now: number,
  launchedAt?: string | number
): HomeRuntime {
  const timestamp =
    typeof launchedAt === 'number'
      ? launchedAt * 1000
      : launchedAt
        ? Date.parse(launchedAt)
        : Number.NaN
  const total = Number.isFinite(timestamp)
    ? Math.max(0, Math.floor((now - timestamp) / 1000))
    : 0
  return {
    days: Math.floor(total / 86_400),
    hours: Math.floor((total % 86_400) / 3_600),
    minutes: Math.floor((total % 3_600) / 60),
    seconds: total % 60,
  }
}

export function useHomeShowcase(
  startTime?: Readonly<Ref<number | null>>,
  options: { initiallyVisible?: boolean } = {}
) {
  const now = ref(Date.now()),
    sectionVisible = ref(options.initiallyVisible ?? true)
  let timer: number | null = null,
    disposed = false
  const runtime = computed(() =>
    calculateRuntime(now.value, startTime?.value ?? undefined)
  )
  function stop() {
    if (timer !== null) window.clearInterval(timer)
    timer = null
  }
  function sync() {
    stop()
    if (
      disposed ||
      !sectionVisible.value ||
      document.visibilityState === 'hidden'
    )
      return
    if (!window.matchMedia('(prefers-reduced-motion: reduce)').matches)
      timer = window.setInterval(() => {
        now.value = Date.now()
      }, 1000)
  }
  function setSectionVisible(value: boolean) {
    if (value === sectionVisible.value) return
    sectionVisible.value = value
    sync()
  }
  function visibility() {
    now.value = Date.now()
    sync()
  }
  function dispose() {
    if (disposed) return
    disposed = true
    stop()
    document.removeEventListener('visibilitychange', visibility)
  }
  document.addEventListener('visibilitychange', visibility)
  sync()
  if (getCurrentScope()) onScopeDispose(dispose)
  return { runtime, setSectionVisible, dispose }
}
