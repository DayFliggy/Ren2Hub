import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const consoleApi = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/console', () => ({ api: consoleApi }))

import { useAuthStore } from '@/stores/auth'
import { useTicketQueueStore } from '@/stores/ticketQueue'

describe('ticket queue summary refresh', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.clearAllMocks()
    consoleApi.get.mockResolvedValue({ pending: 3, unassigned: 2, mine: 1 })
    vi.spyOn(useAuthStore(), 'hasPermission').mockReturnValue(true)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('refreshes on start, every minute, and when the window regains focus', async () => {
    const store = useTicketQueueStore()
    store.start()
    await flushPromises()

    expect(consoleApi.get).toHaveBeenCalledTimes(1)
    expect(store.summary).toEqual({ pending: 3, unassigned: 2, mine: 1 })

    await vi.advanceTimersByTimeAsync(60_000)
    await flushPromises()
    expect(consoleApi.get).toHaveBeenCalledTimes(2)

    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(consoleApi.get).toHaveBeenCalledTimes(3)

    store.stop()
  })

  it('does not query the summary without ticket read permission', async () => {
    vi.mocked(useAuthStore().hasPermission).mockReturnValue(false)
    const store = useTicketQueueStore()

    await store.refresh()

    expect(consoleApi.get).not.toHaveBeenCalled()
    expect(store.summary).toEqual({ pending: 0, unassigned: 0, mine: 0 })
  })

  it('records refresh failures without replacing the last known summary', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const store = useTicketQueueStore()
    await store.refresh()
    consoleApi.get.mockRejectedValueOnce(new Error('network unavailable'))

    await store.refresh()

    expect(store.summary).toEqual({ pending: 3, unassigned: 2, mine: 1 })
    expect(consoleError).toHaveBeenCalledWith(
      'Failed to refresh ticket queue summary',
      expect.any(Error)
    )
  })
})

describe('ticket queue permission and lifetime boundaries', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.spyOn(useAuthStore(), 'hasPermission').mockReturnValue(true)
  })

  it('aborts and ignores an in-flight summary after read permission is revoked', async () => {
    let resolve!: (value: unknown) => void
    consoleApi.get.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      })
    )
    const store = useTicketQueueStore()
    const pending = store.refresh()
    const signal = consoleApi.get.mock.calls[0]![2].signal as AbortSignal
    vi.mocked(useAuthStore().hasPermission).mockReturnValue(false)
    await store.refresh()
    expect(signal.aborted).toBe(true)
    resolve({ pending: 99, unassigned: 99, mine: 99 })
    await pending
    expect(store.summary).toEqual({ pending: 0, unassigned: 0, mine: 0 })
  })

  it('keeps the new poll active when a stopped poll finishes later', async () => {
    let resolveOld!: (value: unknown) => void
    let resolveNew!: (value: unknown) => void
    consoleApi.get.mockReturnValueOnce(
      new Promise((done) => {
        resolveOld = done
      })
    )
    consoleApi.get.mockReturnValueOnce(
      new Promise((done) => {
        resolveNew = done
      })
    )
    const store = useTicketQueueStore()
    store.start()
    store.stop()
    store.start()
    resolveOld({ pending: 99, unassigned: 99, mine: 99 })
    await flushPromises()
    const pending = store.refresh()
    expect(consoleApi.get).toHaveBeenCalledTimes(2)
    resolveNew({ pending: 1, unassigned: 0, mine: 1 })
    await pending
    expect(store.summary).toEqual({ pending: 1, unassigned: 0, mine: 1 })
    store.stop()
  })
})
