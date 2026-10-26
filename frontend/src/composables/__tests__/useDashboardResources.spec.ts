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
import { ref } from 'vue'
import { systemStatusApi, type SystemStatus } from '@/api/systemStatus'
import { ApiError } from '@/api/types'
import { useDashboardResources } from '@/composables/useDashboardResources'
import { systemStatusFixture } from '@/components/console/dashboard/__tests__/systemStatusFixture'
import { useAuthStore } from '@/stores/auth'
import i18n, { loadMessageDomain } from '@/i18n'

const visibility = ref<DocumentVisibilityState>('visible')
vi.mock('@/api/systemStatus', () => ({ systemStatusApi: { get: vi.fn() } }))
vi.mock('@vueuse/core', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@vueuse/core')>()),
  useDocumentVisibility: () => visibility,
}))
const getStatus = vi.mocked(systemStatusApi.get)
const snapshot = systemStatusFixture
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
  visibility.value = 'visible'
  vi.useFakeTimers()
  setActivePinia(createPinia())
  getStatus.mockResolvedValue(snapshot())
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.resetAllMocks()
  vi.useRealTimers()
})

describe('useDashboardResources', () => {
  it.each([1, 10, 100])(
    'reads and polls the shared snapshot for role %s',
    async (role) => {
      setup(role)
      await flushPromises()
      expect(getStatus).toHaveBeenCalledTimes(1)
      expect(state.system.value).toEqual(snapshot())
      await vi.advanceTimersByTimeAsync(10_000)
      expect(getStatus).toHaveBeenCalledTimes(2)
    }
  )
  it('pauses polling while the page is hidden and refreshes on return', async () => {
    setup()
    await flushPromises()
    visibility.value = 'hidden'
    await nextTick()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(getStatus).toHaveBeenCalledTimes(1)
    visibility.value = 'visible'
    await nextTick()
    await flushPromises()
    expect(getStatus).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(getStatus).toHaveBeenCalledTimes(3)
  })
  it('names a rate-limited refresh instead of showing the raw error', async () => {
    getStatus.mockRejectedValueOnce(
      new ApiError('Too Many Requests', { status: 429 })
    )
    setup()
    await flushPromises()
    expect(state.systemError.value).toBe(
      'Refreshing too often. Try again shortly.'
    )
    expect(state.system.value).toBeNull()
  })
  it('starts no snapshot request for anonymous sessions', async () => {
    setup(null)
    await flushPromises()
    expect(getStatus).not.toHaveBeenCalled()
  })
  it('does not start overlapping polling while a snapshot request is pending', async () => {
    const pending = deferred<SystemStatus>()
    getStatus.mockReturnValue(pending.promise)
    setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(getStatus).toHaveBeenCalledTimes(1)
    expect(state.systemLoading.value).toBe(true)
    pending.resolve(snapshot())
    await flushPromises()
    expect(state.systemLoading.value).toBe(false)
  })
  it('surfaces a failed snapshot and recovers on retry', async () => {
    getStatus.mockRejectedValueOnce(new Error('snapshot failed'))
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
      const old = deferred<SystemStatus>()
      getStatus.mockReturnValueOnce(old.promise)
      setup()
      await flushPromises()
      const firstSignal = getStatus.mock.calls[0]![0]!
      await state.loadSystem()
      const current = state.system.value
      expect(firstSignal.aborted).toBe(true)
      if (outcome === 'success') {
        const obsolete = snapshot()
        obsolete.cpu.percent = 99
        old.resolve(obsolete)
      } else old.reject(new Error('obsolete failure'))
      await flushPromises()
      expect(state.system.value).toEqual(current)
      expect(state.systemError.value).toBeNull()
      expect(state.systemLoading.value).toBe(false)
    }
  )
  it('drops the previous account snapshot and aborts its read on account switch', async () => {
    const pending = deferred<SystemStatus>()
    setup()
    await flushPromises()
    getStatus.mockReturnValueOnce(pending.promise)
    const reading = state.loadSystem()
    const signal = getStatus.mock.calls[1]![0]!
    const auth = useAuthStore()
    auth.persist({ id: 2, role: 1 } as never)
    await nextTick()
    expect(signal.aborted).toBe(true)
    pending.resolve(snapshot())
    await reading
    await flushPromises()
    expect(getStatus).toHaveBeenCalledTimes(3)
  })
  it('stops reading after sign-out', async () => {
    setup()
    await flushPromises()
    useAuthStore().persist(null)
    await nextTick()
    expect(state.system.value).toBeNull()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(getStatus).toHaveBeenCalledTimes(1)
  })
  it('aborts the active snapshot read and disposes polling when the Dashboard unmounts', async () => {
    const pendingStats = deferred<SystemStatus>()
    getStatus.mockReturnValue(pendingStats.promise)
    setup()
    await flushPromises()
    const systemSignal = getStatus.mock.calls[0]![0]!
    wrapper!.unmount()
    wrapper = undefined
    expect(systemSignal.aborted).toBe(true)
    pendingStats.resolve(snapshot())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(state.system.value).toBeNull()
    expect(getStatus).toHaveBeenCalledTimes(1)
  })
})
