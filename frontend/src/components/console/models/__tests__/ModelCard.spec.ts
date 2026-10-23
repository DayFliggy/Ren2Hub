import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import ModelCard from '../ModelCard.vue'
import ModelPriceDisplay from '../ModelPriceDisplay.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
const noSamples = { status: 'empty' } as const
describe('native catalog cards', () => {
  it('separates baseline and channel prices without fabricating measured performance', async () => {
    const model = buildModelMarketCatalog(modelCatalogFixture()).models[0]!
    const wrapper = mount(ModelCard, {
      props: { model, performance: noSamples },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-market-baseline]').text()).toContain('$1.250')
    expect(wrapper.get('[data-channel-quote]').text()).toContain('$0.250')
    expect(wrapper.get('[data-channel-quote]').text()).toContain(
      'Fixture channel'
    )
    // Model-wide metrics never sit inside the channel lane they do not describe.
    expect(
      wrapper.find('[data-channel-quote] [data-market-telemetry]').exists()
    ).toBe(false)
    expect(wrapper.get('[data-model-statistics="empty"]').text()).toContain(
      '模型统计'
    )
    expect(wrapper.text()).not.toMatch(/\d+%|TPS|优惠|最低|最优/)
    await wrapper.get('button[aria-label*="模型详情"]').trigger('click')
    expect(wrapper.emitted('detail')).toEqual([
      ['fixture-model', 'fixture-model:9'],
    ])
    wrapper.unmount()
  })
  it('shows unavailable prices without fake zero prices or capability placeholders', () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.price_status = 'unavailable'
    fixture.models[0]!.pricing_values = {}
    fixture.offers[0]!.price_status = 'unavailable'
    fixture.offers[0]!.pricing_values = {}
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelCard, {
      props: { model, performance: noSamples },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('价格不可静态确定')
    expect(wrapper.text()).not.toContain('$0.000')
    expect(wrapper.text()).not.toMatch(/未提供|上下文|工具调用/)
    wrapper.unmount()
  })
  it('renders cache, image and audio dimensions without dropping their price', () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.pricing_values = {
      input: '1',
      cache: '0.25',
      cache_create: '1.25',
      cache_create_1h: '2',
      image: '3',
      image_cache: '0.5',
      image_output: '4',
      audio_input: '5',
      audio_output: '10',
    }
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelPriceDisplay, {
      props: { pricing: model.baselines[0], mode: 'full' },
      global: { plugins: [i18n] },
    })
    for (const dimension of [
      'cacheRead',
      'cacheWrite',
      'cacheWrite1h',
      'image',
      'imageCache',
      'imageOutput',
      'audioInput',
      'audioOutput',
    ]) {
      const line = wrapper.get(`[data-price-dimension="${dimension}"]`)
      expect(line.text()).toContain('$')
      expect(line.text()).not.toContain(dimension)
    }
    wrapper.unmount()
  })
  it.each(['expression_requires_usage_context', 'private_internal_error'])(
    'uses public wording for unavailable reason %s',
    (reason) => {
      const fixture = modelCatalogFixture()
      fixture.offers[0]!.price_status = 'unavailable'
      fixture.offers[0]!.pricing_values = {}
      fixture.offers[0]!.price_reason = reason
      const model = buildModelMarketCatalog(fixture).models[0]!
      const wrapper = mount(ModelPriceDisplay, {
        props: { pricing: model.offers[0]!.pricing },
        global: { plugins: [i18n] },
      })
      expect(wrapper.text()).not.toContain(reason)
      expect(wrapper.text()).not.toContain('$0')
      expect(wrapper.text()).toMatch(/价格|用量/)
      wrapper.unmount()
    }
  )
  it.each([
    ['zh-CN', 'invalid_audio_price', '音频价格超出可显示范围'],
    ['zh-CN', 'protocol_dependent_audio_price', '音频价格因调用协议而异'],
    [
      'en',
      'invalid_audio_price',
      'The audio price exceeds the displayable range',
    ],
    [
      'en',
      'protocol_dependent_audio_price',
      'Audio prices differ by request protocol',
    ],
  ] as const)(
    'localizes specialized unavailable reason %s / %s',
    async (locale, reason, message) => {
      await setLocale(locale)
      const fixture = modelCatalogFixture()
      fixture.offers[0]!.price_status = 'unavailable'
      fixture.offers[0]!.pricing_values = {}
      fixture.offers[0]!.price_reason = reason
      const projected =
        buildModelMarketCatalog(fixture).models[0]!.offers[0]!.pricing
      const wrapper = mount(ModelPriceDisplay, {
        props: { pricing: projected },
        global: { plugins: [i18n] },
      })
      expect(wrapper.text()).toContain(message)
      expect(wrapper.text()).not.toContain(reason)
      expect(wrapper.text()).not.toContain('$0')
      wrapper.unmount()
      await setLocale('zh-CN')
    }
  )
  it('retains an explicitly zero price and omits unsupported billing dimensions', () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.price_status = 'zero'
    fixture.models[0]!.pricing_values = { input: '0', output: '0' }
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelPriceDisplay, {
      props: { pricing: model.baselines[0], mode: 'full' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-price-dimension="input"]').text()).toContain(
      '$0.000'
    )
    expect(wrapper.find('[data-price-dimension="cacheRead"]').exists()).toBe(
      false
    )
    wrapper.unmount()
  })
})
