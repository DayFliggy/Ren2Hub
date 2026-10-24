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
import { getRequestLimits, type RequestLimits } from '@/api/dashboardResources'
import { opsMonitorApi, type PerformanceStats } from '@/api/opsMonitor'
import { useDashboardResources } from '@/composables/useDashboardResources'
import { observabilityFixture } from '@/components/console/dashboard/__tests__/observabilityFixture'
import { useAuthStore } from '@/stores/auth'
import i18n, { loadMessageDomain } from '@/i18n'

vi.mock('@/api/dashboardResources', () => ({ getRequestLimits: vi.fn() }))
vi.mock('@/api/opsMonitor', () => ({ opsMonitorApi: { stats: vi.fn() } }))
const limits: RequestLimits = {
  enabled: true,
  window_minutes: 5,
  total_requests: 300,
  successful_requests: 200,
}
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
  vi.mocked(getRequestLimits).mockResolvedValue(limits)
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
    'loads self limits for role %s without calling Root observability',
    async (role) => {
      setup(role)
      await flushPromises()
      expect(state.limits.value).toEqual(limits)
      expect(opsMonitorApi.stats).not.toHaveBeenCalled()
      await state.loadSystem()
      await vi.advanceTimersByTimeAsync(30_000)
      expect(opsMonitorApi.stats).not.toHaveBeenCalled()
      expect(state.system.value).toBeNull()
    }
  )
  it('starts neither supplemental request for anonymous sessions', async () => {
    setup(null)
    await flushPromises()
    expect(getRequestLimits).not.toHaveBeenCalled()
    expect(opsMonitorApi.stats).not.toHaveBeenCalled()
  })
  it('uses the server snapshot directly for Root and polls only that endpoint', async () => {
    const result = snapshot()
    vi.mocked(opsMonitorApi.stats).mockResolvedValue(result)
    setup()
    await flushPromises()
    expect(state.system.value).toEqual(result.observability)
    expect(state.limits.value).toEqual(limits)
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(2)
    expect(getRequestLimits).toHaveBeenCalledTimes(1)
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
  it('keeps Root metrics available when the independent limit request fails', async () => {
    vi.mocked(getRequestLimits).mockRejectedValueOnce(
      new Error('limits failed')
    )
    setup()
    await flushPromises()
    expect(state.limits.value).toBeNull()
    expect(state.limitsError.value).toBe('limits failed')
    expect(state.system.value).not.toBeNull()
    expect(state.systemError.value).toBeNull()
    await state.loadLimits()
    expect(state.limits.value).toEqual(limits)
    expect(state.limitsError.value).toBeNull()
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(1)
  })
  it('keeps limits available while a snapshot fails and retries independently', async () => {
    vi.mocked(opsMonitorApi.stats).mockRejectedValueOnce(
      new Error('snapshot failed')
    )
    setup()
    await flushPromises()
    expect(state.limits.value).toEqual(limits)
    expect(state.systemError.value).toBe('snapshot failed')
    expect(state.system.value).toBeNull()
    await state.loadSystem()
    expect(state.system.value).not.toBeNull()
    expect(state.systemError.value).toBeNull()
    expect(getRequestLimits).toHaveBeenCalledTimes(1)
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
  it('does not apply a previous account limit result after logout', async () => {
    const pending = deferred<RequestLimits>()
    vi.mocked(getRequestLimits).mockReturnValueOnce(pending.promise)
    const auth = setup(1)
    await flushPromises()
    const signal = vi.mocked(getRequestLimits).mock.calls[0]![0]!
    auth.persist(null)
    await nextTick()
    expect(signal.aborted).toBe(true)
    pending.resolve(limits)
    await flushPromises()
    expect(state.limits.value).toBeNull()
    expect(state.limitsError.value).toBeNull()
    expect(state.limitsLoading.value).toBe(false)
  })
  it('aborts both active reads and disposes polling when the Dashboard unmounts', async () => {
    const pendingLimits = deferred<RequestLimits>()
    const pendingStats = deferred<PerformanceStats>()
    vi.mocked(getRequestLimits).mockReturnValue(pendingLimits.promise)
    vi.mocked(opsMonitorApi.stats).mockReturnValue(pendingStats.promise)
    setup()
    await flushPromises()
    const limitSignal = vi.mocked(getRequestLimits).mock.calls[0]![0]!
    const systemSignal = vi.mocked(opsMonitorApi.stats).mock.calls[0]![0]!
    wrapper!.unmount()
    wrapper = undefined
    expect(limitSignal.aborted).toBe(true)
    expect(systemSignal.aborted).toBe(true)
    pendingLimits.resolve(limits)
    pendingStats.resolve(snapshot())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(state.limits.value).toBeNull()
    expect(state.system.value).toBeNull()
    expect(opsMonitorApi.stats).toHaveBeenCalledTimes(1)
  })
})
