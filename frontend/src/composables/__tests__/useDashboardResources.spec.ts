import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { opsMonitorApi, type PerformanceStats } from '@/api/opsMonitor'
import { useDashboardResources } from '@/composables/useDashboardResources'
import { observabilityFixture } from '@/components/console/dashboard/__tests__/observabilityFixture'
import { useAuthStore } from '@/stores/auth'
import i18n, { loadMessageDomain } from '@/i18n'

vi.mock('@/api/opsMonitor', () => ({ opsMonitorApi: { stats: vi.fn() } }))
function snapshot() {
  return { observability: observabilityFixture() } as PerformanceStats
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
let wrapper: VueWrapper | undefined
let state!: ReturnType<typeof useDashboardResources>
function setup(role: number | null = 100) {
  const auth = useAuthStore()
  auth.persist(role === null ? null : ({ id: 1, role } as never))
  wrapper = mount(
    defineComponent({
      setup() {
        state = useDashboardResources()
        return () => null
      },
    }),
    { global: { plugins: [i18n] } }
  )
  return auth
}
beforeAll(async () => {
  await loadMessageDomain('console')
})
beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  vi.mocked(opsMonitorApi.stats).mockResolvedValue(snapshot())
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.resetAllMocks()
  vi.useRealTimers()
})

describe('useDashboardResources', () => {
  it.each([1, 10])(
    'does not call Root observability for role %s',
    async (role) => {
      setup(role)
      await flushPromises()
      expect(opsMonitorApi.stats).not.toHaveBeenCalled()
      await state.loadSystem()
      await vi.advanceTimersByTimeAsync(30_000)
      expect(opsMonitorApi.stats).not.toHaveBeenCalled()
      expect(state.system.value).toBeNull()
    }
  )
  it('starts no snapshot request for anonymous sessions', async () => {
    setup(null)
    await flushPromises()
    expect(opsMonitorApi.stats).not.toHaveBeenCalled()
  })
  it('uses the server snapshot directly for Root and polls only that endpoint', async () => {
    const result = snapshot()
    vi.mocked(opsMonitorApi.stats).mockResolvedValue(result)
    setup()
    await flushPromises()
    expect(state.system.value).toEqual(result.observability)
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(2)
  })
  it('does not start overlapping polling while a snapshot request is pending', async () => {
    const pending = deferred<PerformanceStats>()
    vi.mocked(opsMonitorApi.stats).mockReturnValue(pending.promise)
    setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(1)
    expect(state.systemLoading.value).toBe(true)
    pending.resolve(snapshot())
    await flushPromises()
    expect(state.systemLoading.value).toBe(false)
  })
  it('surfaces a failed snapshot and recovers on retry', async () => {
    vi.mocked(opsMonitorApi.stats).mockRejectedValueOnce(
      new Error('snapshot failed')
    )
    setup()
    await flushPromises()
    expect(state.systemError.value).toBe('snapshot failed')
    expect(state.system.value).toBeNull()
    await state.loadSystem()
    expect(state.system.value).not.toBeNull()
    expect(state.systemError.value).toBeNull()
  })
  it.each(['success', 'failure'])(
    'ignores a superseded snapshot %s',
    async (outcome) => {
      const old = deferred<PerformanceStats>()
      vi.mocked(opsMonitorApi.stats).mockReturnValueOnce(old.promise)
      setup()
      await flushPromises()
      const firstSignal = vi.mocked(opsMonitorApi.stats).mock.calls[0]![0]!
      await state.loadSystem()
      const current = state.system.value
      expect(firstSignal.aborted).toBe(true)
      if (outcome === 'success') {
        const obsolete = snapshot()
        obsolete.observability.resources.cpu.percent = 99
        old.resolve(obsolete)
      } else old.reject(new Error('obsolete failure'))
      await flushPromises()
      expect(state.system.value).toEqual(current)
      expect(state.systemError.value).toBeNull()
      expect(state.systemLoading.value).toBe(false)
    }
  )
  it('revokes cached Root metrics, aborts pending reads and stops polling on downgrade', async () => {
    const pending = deferred<PerformanceStats>()
    setup()
    await flushPromises()
    vi.mocked(opsMonitorApi.stats).mockReturnValueOnce(pending.promise)
    const reading = state.loadSystem()
    const signal = vi.mocked(opsMonitorApi.stats).mock.calls[1]![0]!
    const auth = useAuthStore()
    auth.persist({ id: 1, role: 10 } as never)
    await nextTick()
    expect(signal.aborted).toBe(true)
    expect(state.system.value).toBeNull()
    expect(state.systemLoading.value).toBe(false)
    pending.resolve(snapshot())
    await reading
    await vi.advanceTimersByTimeAsync(30_000)
    expect(state.system.value).toBeNull()
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(2)
  })
  it('aborts the active snapshot read and disposes polling when the Dashboard unmounts', async () => {
    const pendingStats = deferred<PerformanceStats>()
    vi.mocked(opsMonitorApi.stats).mockReturnValue(pendingStats.promise)
    setup()
    await flushPromises()
    const systemSignal = vi.mocked(opsMonitorApi.stats).mock.calls[0]![0]!
    wrapper!.unmount()
    wrapper = undefined
    expect(systemSignal.aborted).toBe(true)
    pendingStats.resolve(snapshot())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(state.system.value).toBeNull()
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(1)
  })
})
