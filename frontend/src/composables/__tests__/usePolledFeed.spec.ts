import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePolledFeed } from '../usePolledFeed'

let wrapper: VueWrapper | undefined
function host<T>(setup: () => T): T {
  let state!: T
  wrapper = mount(
    defineComponent({
      setup() {
        state = setup()
        return () => null
      },
    })
  )
  return state
}
function setHidden(hidden: boolean) {
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(hidden)
  document.dispatchEvent(new Event('visibilitychange'))
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('polled feed', () => {
  it('isolates failures per feed and marks previously shown data as stale', async () => {
    const healthy = vi.fn().mockResolvedValue('ok')
    const flaky = vi
      .fn()
      .mockResolvedValueOnce('first')
      .mockRejectedValueOnce(new Error('feed down'))
      .mockResolvedValueOnce('recovered')
    const feeds = host(() => ({
      healthy: usePolledFeed(healthy, () => 1000),
      flaky: usePolledFeed(flaky, () => 1000),
    }))
    await flushPromises()
    const firstSuccess = feeds.flaky.updatedAt.value
    await vi.advanceTimersByTimeAsync(1000)
    expect(feeds.flaky.error.value).toBe('feed down')
    expect(feeds.flaky.data.value).toBe('first')
    expect(feeds.flaky.stale.value).toBe(true)
    expect(feeds.flaky.updatedAt.value).toBe(firstSuccess)
    expect(feeds.healthy.error.value).toBe('')
    expect(healthy).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(feeds.flaky.data.value).toBe('recovered')
    expect(feeds.flaky.error.value).toBe('')
    expect(feeds.flaky.stale.value).toBe(false)
  })

  it('reports a first-load failure without inventing data', async () => {
    const feed = host(() =>
      usePolledFeed(
        () => Promise.reject(new Error('down')),
        () => null
      )
    )
    await flushPromises()
    expect(feed.data.value).toBeNull()
    expect(feed.error.value).toBe('down')
    expect(feed.stale.value).toBe(false)
    expect(feed.updatedAt.value).toBe(0)
  })

  it('aborts a superseded request and ignores its late result', async () => {
    const pending: Array<{
      signal: AbortSignal
      resolve: (value: string) => void
    }> = []
    const fetch = vi.fn(
      (signal: AbortSignal) =>
        new Promise<string>((resolve) => pending.push({ signal, resolve }))
    )
    const feed = host(() => usePolledFeed(fetch, () => null))
    await flushPromises()
    void feed.refresh()
    expect(pending[0]!.signal.aborted).toBe(true)
    pending[1]!.resolve('current')
    await flushPromises()
    pending[0]!.resolve('obsolete')
    await flushPromises()
    expect(feed.data.value).toBe('current')
  })

  it('pauses while the document is hidden and refreshes when visible again', async () => {
    let signal: AbortSignal | undefined
    const fetch = vi.fn((requestSignal: AbortSignal) => {
      signal = requestSignal
      return new Promise<string>(() => {})
    })
    const feed = host(() => usePolledFeed(fetch, () => 1000))
    await flushPromises()
    setHidden(true)
    expect(signal?.aborted).toBe(true)
    expect(feed.loading.value).toBe(false)
    await vi.advanceTimersByTimeAsync(5000)
    expect(fetch).toHaveBeenCalledTimes(1)
    fetch.mockResolvedValue('visible')
    setHidden(false)
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(feed.data.value).toBe('visible')
  })

  it('does not request while mounted hidden', async () => {
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    const fetch = vi.fn().mockResolvedValue('x')
    host(() => usePolledFeed(fetch, () => 1000))
    await flushPromises()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('aborts the request and stops polling on unmount', async () => {
    let signal: AbortSignal | undefined
    const fetch = vi.fn((requestSignal: AbortSignal) => {
      signal = requestSignal
      return new Promise<string>(() => {})
    })
    host(() => usePolledFeed(fetch, () => 1000))
    await flushPromises()
    wrapper!.unmount()
    wrapper = undefined
    expect(signal?.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(5000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('lets the interval stop polling based on the latest data', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(['running'])
      .mockResolvedValue([])
    host(() =>
      usePolledFeed(fetch, (rows: string[] | null) =>
        rows?.length ? 8000 : null
      )
    )
    await flushPromises()
    await vi.advanceTimersByTimeAsync(8000)
    expect(fetch).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(fetch).toHaveBeenCalledTimes(2)
  })
})
