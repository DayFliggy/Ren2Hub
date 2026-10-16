import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { api } from '@/api/client'
import { useModelMarket } from '@/composables/useModelMarket'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { createModelMarketFixture } from '../../../tests/fixtures/modelMarket'

const wrappers: VueWrapper[] = []
function harness() {
  let market!: ReturnType<typeof useModelMarket>
  const wrapper = mount(
    defineComponent({
      setup() {
        market = useModelMarket()
        return () => null
      },
    }),
    { global: { plugins: [i18n] } }
  )
  wrappers.push(wrapper)
  return { market, wrapper }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
beforeEach(() => {
  vi.spyOn(api, 'get').mockResolvedValue(createModelMarketFixture())
})
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  vi.restoreAllMocks()
})

describe('model market state and request lifecycle', () => {
  it('loads one authenticated catalog and keeps supply filters when switching all views', async () => {
    const { market } = harness()
    await market.load()
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(api.get).toHaveBeenCalledWith('/api/user/model-market', undefined, {
      signal: expect.any(AbortSignal),
    })
    market.channel.value = 11
    market.keyword.value = 'OpenAI'
    market.billing.value = 'token'
    market.sort.value = 'input'
    const ids = market.filtered.value.map((model) => model.id)
    expect(ids).toEqual(['gpt-4.1', 'audio-preview'])
    for (const view of ['channels', 'comparison', 'models'] as const) {
      market.view.value = view
      await nextTick()
      expect(market.filtered.value.map((model) => model.id)).toEqual(ids)
      expect(
        market.filtered.value.every((model) =>
          model.offers.every((offer) => offer.channelId === 11)
        )
      ).toBe(true)
      expect(market.visibleChannels.value.map((channel) => channel.id)).toEqual(
        [11]
      )
    }
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('searches channel names and keeps mixed billing within the same shelf', async () => {
    const { market } = harness()
    await market.load()
    market.keyword.value = '雾桥'
    expect(market.visibleChannels.value.map((channel) => channel.id)).toEqual([
      11,
    ])
    const modes = new Set(
      market.shelves.value[0]!.items.flatMap((item) =>
        item.offers.map((offer) => offer.pricing.billingMode)
      )
    )
    expect(modes).toEqual(
      new Set(['token', 'per_call', 'dynamic', 'unconfigured'])
    )
    market.billing.value = 'dynamic'
    expect(market.filtered.value.map((model) => model.id)).toEqual([
      'video-preview',
    ])
    expect(market.filtered.value[0]!.offers).toHaveLength(1)
    expect(
      market.filtered.value[0]!.baselines.map((price) => price.id)
    ).toEqual(['video-preview:seconds'])
    const originalVideo = market.catalog.value!.models.find(
      (model) => model.id === 'video-preview'
    )!
    expect(originalVideo.baselines.map((price) => price.id)).toEqual([
      'video-preview:seconds',
      'video-preview:fixed',
    ])
    expect(originalVideo.offers).toHaveLength(4)
    market.view.value = 'comparison'
    market.resetFilters()
    expect(market.resultCount.value).toBe(7)
    expect(
      market.filtered.value
        .find((model) => model.id === 'video-preview')!
        .baselines.map((price) => price.id)
    ).toEqual(['video-preview:seconds', 'video-preview:fixed'])
    expect(
      market.catalog.value!.models.find((model) => model.id === 'video-preview')
    ).toBe(originalVideo)
    expect(market.view.value).toBe('comparison')
  })

  it.each(['grid', 'list'])(
    'migrates persisted %s without storing permission-scoped prices',
    async (oldView) => {
      localStorage.setItem('ren2hub_models_view', oldView)
      const { market } = harness()
      expect(market.view.value).toBe('models')
      await market.load()
      await flushPromises()
      expect(localStorage.getItem('ren2hub_models_view')).toBe('models')
      expect(Object.values(localStorage).join(' ')).not.toMatch(
        /gpt-4\.1|雾桥|ModelRatio/
      )
    }
  )

  it('reports failed refreshes, discards stale authorized data, and retries successfully', async () => {
    const { market } = harness()
    await market.load()
    vi.mocked(api.get).mockRejectedValueOnce(new Error('offline'))
    await market.load()
    expect(market.error.value).not.toBe('')
    expect(market.loading.value).toBe(false)
    expect(market.catalog.value).toBeNull()
    expect(market.hasResults.value).toBe(false)
    await market.load()
    expect(market.error.value).toBe('')
    expect(market.resultCount.value).toBe(7)
  })

  it('aborts older loads and prevents a late response restoring superseded supply', async () => {
    const first = deferred<ReturnType<typeof createModelMarketFixture>>()
    const second = deferred<ReturnType<typeof createModelMarketFixture>>()
    vi.mocked(api.get)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
    const { market } = harness()
    const oldLoad = market.load()
    const firstSignal = vi.mocked(api.get).mock.calls[0]![2]!.signal!
    const currentLoad = market.load()
    expect(firstSignal.aborted).toBe(true)
    const restricted = createModelMarketFixture()
    restricted.offers = restricted.offers.filter(
      (offer) => offer.channel_id === 13
    )
    restricted.channels = restricted.channels.filter(
      (channel) => channel.id === 13
    )
    second.resolve(restricted)
    await currentLoad
    expect(market.visibleChannels.value.map((channel) => channel.id)).toEqual([
      13,
    ])
    first.resolve(createModelMarketFixture())
    await oldLoad
    expect(market.visibleChannels.value.map((channel) => channel.id)).toEqual([
      13,
    ])
  })

  it('aborts an in-flight request when the view unmounts', async () => {
    const pending = deferred<ReturnType<typeof createModelMarketFixture>>()
    vi.mocked(api.get).mockReturnValueOnce(pending.promise)
    const { market, wrapper } = harness()
    const load = market.load()
    const signal = vi.mocked(api.get).mock.calls[0]![2]!.signal!
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    pending.resolve(createModelMarketFixture())
    await load
    expect(market.catalog.value).toBeNull()
  })
})
