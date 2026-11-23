import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useSystemUpdateStore } from '@/stores/systemUpdate'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({
  api: { get: mocks.get },
  setApiUnauthorizedHandler: vi.fn(),
}))

const answer = (latest: string) => ({
  current_version: 'v1.0.1',
  latest_version: latest,
  has_update: latest !== 'v1.0.1',
  build_type: 'release',
  cached: false,
})

function deferred() {
  let resolve: (value: unknown) => void = () => {}
  const promise = new Promise((done) => (resolve = done))
  return { promise, resolve }
}

beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  useAuthStore().persist({ id: 1, role: 100 } as never)
})

describe('system update store', () => {
  it('reads once and serves the loaded result to later unforced checks', async () => {
    mocks.get.mockResolvedValue(answer('v1.0.2'))
    const store = useSystemUpdateStore()
    await store.check(false)
    await store.check(false)
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(mocks.get).toHaveBeenCalledWith(
      '/api/admin/system/check-updates',
      { force: false },
      expect.anything()
    )
    expect(store.info?.latest_version).toBe('v1.0.2')
  })

  it('shares one request between concurrent checks', async () => {
    const reply = deferred()
    mocks.get.mockReturnValue(reply.promise)
    const store = useSystemUpdateStore()
    const first = store.check(false)
    // Even a forced check joins the read that is already running.
    const second = store.check(true)
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(store.loading).toBe(true)
    reply.resolve(answer('v1.0.2'))
    await Promise.all([first, second])
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(store.info?.latest_version).toBe('v1.0.2')
    expect(store.loading).toBe(false)
  })

  it('forces a new read past the loaded result', async () => {
    mocks.get
      .mockResolvedValueOnce(answer('v1.0.2'))
      .mockResolvedValueOnce(answer('v1.0.3'))
    const store = useSystemUpdateStore()
    await store.check(false)
    await store.check(true)
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/check-updates',
      { force: true },
      expect.anything()
    )
    expect(store.info?.latest_version).toBe('v1.0.3')
  })

  it('reads again after the cache is cleared and drops the answer it replaced', async () => {
    const stale = deferred()
    mocks.get
      .mockReturnValueOnce(stale.promise)
      .mockResolvedValueOnce(answer('v1.0.1'))
    const store = useSystemUpdateStore()
    const first = store.check(false)
    store.clear()
    await store.check(false)
    stale.resolve(answer('v1.0.2'))
    await first
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(store.info?.latest_version).toBe('v1.0.1')
    expect(store.loading).toBe(false)
  })

  it('keeps the failure for the panel and retries on the next check', async () => {
    const failure = new ApiError('Bad Gateway', { status: 502 })
    mocks.get
      .mockRejectedValueOnce(failure)
      .mockResolvedValueOnce(answer('v1.0.1'))
    const store = useSystemUpdateStore()
    await store.check(false)
    expect(store.error).toBe(failure)
    expect(store.info).toBeNull()
    await store.check(false)
    expect(store.error).toBeNull()
    expect(store.info?.has_update).toBe(false)
  })

  it('forgets the result when another account signs in', async () => {
    mocks.get.mockResolvedValue(answer('v1.0.2'))
    const store = useSystemUpdateStore()
    await store.check(false)
    useAuthStore().persist({ id: 2, role: 100 } as never)
    await Promise.resolve()
    expect(store.info).toBeNull()
  })
})
