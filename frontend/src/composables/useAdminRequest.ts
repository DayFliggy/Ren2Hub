import { onScopeDispose, ref } from 'vue'

export function useAdminRequest() {
  const loading = ref(false)
  const error = ref('')
  let active: AbortController | undefined
  onScopeDispose(() => active?.abort())
  async function run(task: (signal: AbortSignal) => Promise<void>) {
    active?.abort()
    const controller = new AbortController()
    active = controller
    loading.value = true
    error.value = ''
    try {
      await task(controller.signal)
    } catch (cause) {
      if (!controller.signal.aborted)
        error.value = cause instanceof Error ? cause.message : String(cause)
    } finally {
      if (active === controller) loading.value = false
    }
  }
  return { loading, error, run }
}
