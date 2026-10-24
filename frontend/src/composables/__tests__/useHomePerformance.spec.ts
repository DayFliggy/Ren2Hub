import { effectScope, nextTick } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useHomePerformance } from '../useHomePerformance'
const fetchSummary = vi.hoisted(() => vi.fn())
vi.mock('@/api/modelMarket', () => ({ getPerformanceSummary: fetchSummary }))
vi.mock('@/api/publicCatalog', () => ({
  publicCatalogApi: {
    pricing: vi.fn().mockResolvedValue({ models: [], issues: [] }),
  },
}))
beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().checked = true
  vi.resetAllMocks()
})
function setup() {
  const scope = effectScope()
  const value = scope.run(useHomePerformance)!
  return { scope, value, app: useAppStore(), auth: useAuthStore() }
}
describe('home performance permissions and lifecycle', () => {
  it('waits for status and does not fetch a disabled or protected anonymous summary', async () => {
    const { scope, value, app } = setup()
    expect(value.state.value).toBe('loading')
    expect(fetchSummary).not.toHaveBeenCalled()
    app.status.HeaderNavModules = '{"pricing":false}'
    app.statusReachable = true
    expect(value.state.value).toBe('notPublic')
    app.status.HeaderNavModules = '{"pricing":{"requireAuth":true}}'
    expect(value.state.value).toBe('signIn')
    expect(fetchSummary).not.toHaveBeenCalled()
    scope.stop()
  })
  it('distinguishes zero, no samples and errors without retaining a previous value', async () => {
    const { scope, value, app } = setup()
    fetchSummary.mockResolvedValueOnce({ summary: { avg_latency_ms: 0 } })
    app.statusReachable = true
    await flushPromises()
    expect(value.latency.value).toBe(0)
    expect(value.state.value).toBe('ready')
    fetchSummary.mockResolvedValueOnce({ summary: null })
    app.status.HeaderNavModules = '{"pricing":true}'
    await flushPromises()
    expect(value.latency.value).toBeNull()
    expect(value.state.value).toBe('empty')
    fetchSummary.mockRejectedValueOnce(new Error('offline'))
    app.status.HeaderNavModules = ''
    await flushPromises()
    expect(value.state.value).toBe('error')
    expect(value.error.value).toBeInstanceOf(Error)
    scope.stop()
  })
  it('waits for session discovery, then clears on account switches and logout', async () => {
    const { scope, value, app, auth } = setup()
    auth.checked = false
    app.status.HeaderNavModules = '{"pricing":{"requireAuth":true}}'
    app.statusReachable = true
    expect(value.state.value).toBe('loading')
    expect(fetchSummary).not.toHaveBeenCalled()
    auth.checked = true
    expect(value.state.value).toBe('signIn')
    const pending: Array<(value: unknown) => void> = []
    fetchSummary.mockImplementation(
      () => new Promise((resolve) => pending.push(resolve))
    )
    auth.persist({ id: 1 } as never)
    const firstSignal = fetchSummary.mock.calls[0]![1] as AbortSignal
    auth.persist({ id: 2 } as never)
    expect(firstSignal.aborted).toBe(true)
    expect(value.latency.value).toBeNull()
    pending[0]!({ summary: { avg_latency_ms: 100 } })
    await flushPromises()
    expect(value.latency.value).toBeNull()
    pending[1]!({ summary: { avg_latency_ms: 200 } })
    await flushPromises()
    expect(value.latency.value).toBe(200)
    auth.persist(null)
    expect(value.state.value).toBe('signIn')
    expect(value.latency.value).toBeNull()
    expect(fetchSummary).toHaveBeenCalledTimes(2)
    scope.stop()
  })

  it('aborts and discards results after access changes and scope disposal', async () => {
    const { scope, value, app } = setup()
    let resolve!: (value: unknown) => void
    fetchSummary.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    app.statusReachable = true
    const signal = fetchSummary.mock.calls[0]![1] as AbortSignal
    app.status.HeaderNavModules = '{"pricing":false}'
    expect(signal.aborted).toBe(true)
    resolve({ summary: { avg_latency_ms: 100 } })
    await flushPromises()
    expect(value.latency.value).toBeNull()
    expect(value.state.value).toBe('notPublic')
    fetchSummary.mockResolvedValueOnce({ summary: { avg_latency_ms: 10 } })
    app.status.HeaderNavModules = ''
    await flushPromises()
    expect(value.latency.value).toBe(10)
    scope.stop()
    await nextTick()
    expect(value.latency.value).toBeNull()
  })
})
