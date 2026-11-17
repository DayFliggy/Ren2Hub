import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import HomeShowcase from '../HomeShowcase.vue'

const mocks = vi.hoisted(() => ({ homeMetrics: vi.fn() }))
vi.mock('@/api/public', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/public')>()
  return {
    ...actual,
    publicApi: { ...actual.publicApi, homeMetrics: mocks.homeMetrics },
  }
})

type Callback = (entries: Partial<IntersectionObserverEntry>[]) => void
// The showcase observes its section for metrics and for motion; a scroll
// reaches every observer.
let callbacks: Callback[] = []
let observed: Callback | null = null
class RecordingObserver {
  constructor(callback: Callback) {
    callbacks.push(callback)
    observed = (entries) => callbacks.forEach((each) => each(entries))
  }
  observe() {}
  disconnect() {}
  unobserve() {}
  takeRecords() {
    return []
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  callbacks = []
  observed = null
  vi.stubGlobal('IntersectionObserver', RecordingObserver)
  mocks.homeMetrics.mockResolvedValue({
    available: true,
    generated_at: 1,
    requests_24h: 752,
    hourly_requests: Array(24).fill(0),
  })
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('HomeShowcase visibility', () => {
  it('loads request metrics when a batched observer callback ends visible', async () => {
    const wrapper = mount(HomeShowcase, {
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', messages: {} })],
        stubs: { RuntimePulseBand: true },
      },
    })
    await flushPromises()
    expect(observed).not.toBeNull()
    expect(mocks.homeMetrics).not.toHaveBeenCalled()
    // The observer may deliver the initial hidden record together with the
    // visible one after a scroll; only the newest record is the current state.
    observed!([{ isIntersecting: false }, { isIntersecting: true }])
    await flushPromises()
    expect(mocks.homeMetrics).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
