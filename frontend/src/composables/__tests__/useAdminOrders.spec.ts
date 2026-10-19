import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getTopUps } from '@/api/adminOrders'
import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { useAdminOrders } from '@/composables/useAdminOrders'
import type { AdminOrderRange, AdminOrderStats } from '@/types/console'

vi.mock('@/api/adminOrders', () => ({ getTopUps: vi.fn() }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn() }),
}))

const now = 1_792_368_000
const initialStats: AdminOrderStats = {
  total: 4,
  currencies: [
    {
      currency: 'CNY',
      count: 4,
      paid_count: 3,
      paid_amount: '73.00',
      credited_quota: 500,
      unknown_amount_count: 0,
    },
  ],
  daily: [],
  start_time: now - 30 * 86400,
  end_time: now,
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

let wrapper: VueWrapper | undefined
async function setup() {
  let state!: ReturnType<typeof useAdminOrders>
  wrapper = mount(
    defineComponent({
      setup() {
        state = useAdminOrders()
        return () => null
      },
    })
  )
  await flushPromises()
  return state
}

beforeEach(() => {
  vi.spyOn(Date, 'now').mockReturnValue(now * 1000)
  vi.mocked(getTopUps).mockResolvedValue({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})

describe('order statistics snapshot identity', () => {
  it.each([7, 90] as AdminOrderRange[])(
    'clears the 30-day snapshot immediately when switching to %s days and failing',
    async (range) => {
      const pending = deferred<AdminOrderStats>()
      const get = vi
        .spyOn(api, 'get')
        .mockResolvedValueOnce(initialStats)
        .mockImplementationOnce(() => pending.promise as never)
      const state = await setup()
      expect(state.stats.value).toEqual(initialStats)

      state.range.value = range
      expect(state.stats.value).toBeNull()
      expect(state.statsLoading.value).toBe(true)
      expect(get).toHaveBeenLastCalledWith(
        '/api/user/topup/stats',
        { start_time: now - range * 86400, end_time: now },
        { signal: expect.any(AbortSignal) }
      )

      pending.reject(new ApiError('Statistics unavailable'))
      await flushPromises()
      expect(state.stats.value).toBeNull()
      expect(state.statsError.value).toBe('Statistics unavailable')
      expect(state.statsStale.value).toBe(false)
      expect(state.statsLoading.value).toBe(false)
    }
  )

  it('marks a retained same-range snapshot stale until a successful refresh', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce(initialStats)
    const state = await setup()
    get.mockRejectedValueOnce(new ApiError('Refresh failed'))

    await state.loadStats({ background: true })
    expect(state.stats.value).toEqual(initialStats)
    expect(state.statsError.value).toBe('Refresh failed')
    expect(state.statsStale.value).toBe(true)
    expect(state.statsRefreshing.value).toBe(false)

    const pending = deferred<AdminOrderStats>()
    get.mockImplementationOnce(() => pending.promise as never)
    const refresh = state.loadStats({ background: true })
    expect(state.statsRefreshing.value).toBe(true)
    expect(state.statsStale.value).toBe(true)
    const updated = { ...initialStats, total: 5 }
    pending.resolve(updated)
    await refresh
    expect(state.stats.value).toEqual(updated)
    expect(state.statsError.value).toBe('')
    expect(state.statsStale.value).toBe(false)
  })

  it('does not restore an aborted older range when its response arrives late', async () => {
    const oldRange = deferred<AdminOrderStats>()
    const newRange = deferred<AdminOrderStats>()
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce(initialStats)
      .mockImplementationOnce(() => oldRange.promise as never)
      .mockImplementationOnce(() => newRange.promise as never)
    const state = await setup()
    state.range.value = 7
    state.range.value = 90
    expect(get.mock.calls[1]?.[2]?.signal?.aborted).toBe(true)

    const current = { ...initialStats, total: 90 }
    newRange.resolve(current)
    await flushPromises()
    oldRange.resolve({ ...initialStats, total: 7 })
    await flushPromises()
    expect(state.stats.value).toEqual(current)
    expect(state.statsError.value).toBe('')
    expect(state.statsStale.value).toBe(false)
  })

  it.each([401, 403])(
    'clears the retained snapshot when a refresh is denied with HTTP %s',
    async (status) => {
      const get = vi.spyOn(api, 'get').mockResolvedValueOnce(initialStats)
      const state = await setup()
      get.mockRejectedValueOnce(new ApiError('Access denied', { status }))
      await state.loadStats({ background: true })
      expect(state.stats.value).toBeNull()
      expect(state.statsError.value).toBe('Access denied')
      expect(state.statsStale.value).toBe(false)
    }
  )

  it('aborts an unmounted statistics request and ignores a transport that resolves anyway', async () => {
    const pending = deferred<AdminOrderStats>()
    const get = vi
      .spyOn(api, 'get')
      .mockImplementationOnce(() => pending.promise as never)
    const state = await setup()
    wrapper?.unmount()
    wrapper = undefined
    expect(get.mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    pending.resolve(initialStats)
    await flushPromises()
    expect(state.stats.value).toBeNull()
  })
})
