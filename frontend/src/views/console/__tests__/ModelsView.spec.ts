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
import { api } from '@/api/client'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import ModelsView from '@/views/console/ModelsView.vue'
import { createModelMarketFixture } from '../../../../tests/fixtures/modelMarket'

const wrappers: VueWrapper[] = []
function render() {
  const wrapper = mount(ModelsView, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  wrappers.push(wrapper)
  return wrapper
}
function dialog() {
  return document.body.querySelector<HTMLElement>('[role="dialog"]')!
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

describe('ModelsView authenticated market integration', () => {
  it('loads a single catalog and keeps the current search through all three modes', async () => {
    const wrapper = render()
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/api/user/model-market', undefined, {
      signal: expect.any(AbortSignal),
    })
    expect(wrapper.findAll('[data-model-card]')).toHaveLength(7)
    await wrapper.get('input[name="model-search"]').setValue('gpt-4.1')
    expect(wrapper.findAll('[data-model-card]')).toHaveLength(1)
    for (const label of ['渠道货架', '比价矩阵', '模型聚合']) {
      await wrapper
        .get(`[role="radio"][aria-label="${label}"]`)
        .trigger('click')
      expect(
        (wrapper.get('input[name="model-search"]').element as HTMLInputElement)
          .value
      ).toBe('gpt-4.1')
      expect(wrapper.text()).toContain('1 个模型')
    }
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('keeps full baseline/cache prices separate from channel statistics in details', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper
      .get('[data-model-name="gpt-4.1"] button[aria-label*="模型详情"]')
      .trigger('click')
    await flushPromises()
    const popup = dialog()
    expect(popup).not.toBeNull()
    expect(
      popup.querySelector('[data-price-dimension="cacheRead"]')!.textContent
    ).toContain('$0.125')
    expect(
      popup.querySelector('[data-price-dimension="cacheWrite"]')!.textContent
    ).toContain('$0.625')
    expect(popup.textContent).toContain('/v1/chat/completions')
    expect(
      popup.querySelector('[data-market-baseline] [data-market-telemetry]')
    ).toBeNull()
    expect(
      popup.querySelector('[data-market-telemetry]')!.textContent
    ).toContain('统计')
    popup
      .querySelector<HTMLButtonElement>('button[data-market-baseline]')!
      .click()
    await flushPromises()
    expect(
      dialog().querySelector('[data-price-dimension="cacheRead"]')!.textContent
    ).toContain('$0.625')
    expect(
      dialog().querySelector('[data-price-dimension="cacheWrite"]')!.textContent
    ).toContain('$3.125')
    dialog()
      .querySelector<HTMLButtonElement>('button[aria-label="关闭模型详情"]')!
      .click()
    await flushPromises()
    await vi.waitFor(() =>
      expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    )
  })

  it('switches plugin variants with matching platform and channel pricing and exposes the full expression', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper
      .get('[data-model-name="video-preview"] button[aria-label*="模型详情"]')
      .trigger('click')
    await flushPromises()
    expect(dialog().querySelector('pre')!.textContent).toBe(
      'u("seconds") * 0.2'
    )
    expect(dialog().textContent).toContain('seconds')
    expect(dialog().textContent).toContain('/v1/videos')
    const fixedOffer = createModelMarketFixture().offers.find(
      (offer) =>
        offer.model_id === 'video-preview' &&
        offer.channel_id === 11 &&
        offer.pricing_id === 'video-preview:fixed'
    )!
    dialog()
      .querySelector<HTMLButtonElement>(
        `button[data-offer-id="${fixedOffer.id}"]`
      )!
      .click()
    await flushPromises()
    expect(dialog().querySelector('pre')).toBeNull()
    expect(
      dialog().querySelector(
        '.market-detail-price-panel [data-price-dimension="request"]'
      )!.textContent
    ).toContain('$0.016')
  })

  it('distinguishes an empty supply catalog from a search with no matches', async () => {
    const empty = createModelMarketFixture()
    empty.offers = []
    vi.mocked(api.get).mockResolvedValueOnce(empty)
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('暂无模型供给')
    expect(wrapper.text()).not.toContain('清除筛选')
    wrapper.unmount()
    const loaded = render()
    await flushPromises()
    await loaded.get('input[name="model-search"]').setValue('no matching model')
    expect(loaded.text()).toContain('没有匹配的模型')
    expect(loaded.text()).toContain('清除筛选')
  })

  it('reports loading failure and restores catalog content through retry', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('模型广场加载失败')
    expect(wrapper.findAll('[data-model-card]')).toHaveLength(0)
    const retry = wrapper
      .findAll('button')
      .find((button) => button.text() === '重新加载')!
    await retry.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('模型广场加载失败')
    expect(wrapper.findAll('[data-model-card]')).toHaveLength(7)
    expect(api.get).toHaveBeenCalledTimes(2)
  })
})
