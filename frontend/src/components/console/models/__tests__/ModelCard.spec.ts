import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import ModelCard from '@/components/console/models/ModelCard.vue'
import ModelPriceDisplay from '@/components/console/models/ModelPriceDisplay.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import { createModelMarketFixture } from '../../../../../tests/fixtures/modelMarket'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
function model(id = 'gpt-4.1') {
  return buildModelMarketCatalog(createModelMarketFixture()).models.find(
    (item) => item.id === id
  )!
}

describe('model aggregation card', () => {
  it('separates the platform baseline from the selected channel quote without promotional or fabricated metrics', () => {
    const wrapper = mount(ModelCard, {
      props: { model: model() },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-market-baseline]').text()).toContain('平台基准价')
    expect(wrapper.get('[data-market-baseline]').text()).toContain('$2.500')
    expect(wrapper.get('[data-channel-quote]').text()).toContain('雾桥中转')
    expect(wrapper.get('[data-channel-quote]').text()).toContain('$0.500')
    expect(wrapper.get('[data-market-telemetry]').text()).toContain('统计')
    expect(
      wrapper
        .get('[data-market-baseline]')
        .find('[data-market-telemetry]')
        .exists()
    ).toBe(false)
    expect(wrapper.text()).not.toMatch(
      /TPS|耗时|延迟|可用率|\d+%|优惠|最低|最优/
    )
    wrapper.unmount()
  })

  it('opens the same offer that is shown as the representative quote', async () => {
    const selected = model()
    selected.offers = [selected.offers[1]!]
    const wrapper = mount(ModelCard, {
      props: { model: selected },
      global: { plugins: [i18n] },
    })
    await wrapper.get('button[aria-label*="模型详情"]').trigger('click')
    expect(wrapper.emitted('detail')).toEqual([
      [selected.id, selected.offers[0]!.id],
    ])
    expect(wrapper.get('[data-channel-quote]').text()).toContain('原生专线')
    wrapper.unmount()
  })

  it('marks unconfigured supply instead of rendering a free price', () => {
    const wrapper = mount(ModelCard, {
      props: { model: model('unpriced-preview') },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('价格未配置')
    expect(wrapper.text()).not.toContain('$0.000')
    wrapper.unmount()
  })
})

describe('shared market price display', () => {
  it('preserves cache zero and omits missing dimensions in full price details', () => {
    const wrapper = mount(ModelPriceDisplay, {
      props: { pricing: model('claude-sonnet-4').baselines[0], mode: 'full' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-price-dimension="cacheRead"]').text()).toContain(
      '$0.300'
    )
    expect(wrapper.get('[data-price-dimension="cacheWrite"]').text()).toContain(
      '$0.000'
    )
    expect(wrapper.find('[data-price-dimension="audioInput"]').exists()).toBe(
      false
    )
    wrapper.unmount()
  })

  it('exposes audio input and output as separate token dimensions', () => {
    const wrapper = mount(ModelPriceDisplay, {
      props: {
        pricing: model('audio-preview').offers[0]!.pricing,
        mode: 'full',
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-price-dimension="audioInput"]').text()).toContain(
      '$8.000'
    )
    expect(
      wrapper.get('[data-price-dimension="audioOutput"]').text()
    ).toContain('$16.000')
    expect(wrapper.text()).toContain('/ 1M tokens')
    wrapper.unmount()
  })

  it('prioritizes audio dimensions in the summary for audio-only endpoints', () => {
    const wrapper = mount(ModelPriceDisplay, {
      props: { pricing: model('audio-preview').offers[0]!.pricing },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-price-dimension="audioInput"]').text()).toContain(
      '$8.000'
    )
    expect(
      wrapper.get('[data-price-dimension="audioOutput"]').text()
    ).toContain('$16.000')
    expect(wrapper.find('[data-price-dimension="input"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the exact dynamic expression without inventing a token unit price', () => {
    const wrapper = mount(ModelPriceDisplay, {
      props: {
        pricing: model('video-preview').offers[0]!.pricing,
        mode: 'full',
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('pre').text()).toBe('u("seconds") * 0.2')
    expect(wrapper.text()).not.toMatch(/\$|1M tokens/)
    wrapper.unmount()
  })
})
