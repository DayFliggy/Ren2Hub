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
describe('native catalog cards', () => {
  it('separates baseline and channel prices without fabricating measured performance', async () => {
    const model = buildModelMarketCatalog(modelCatalogFixture()).models[0]!
    const wrapper = mount(ModelCard, {
      props: { model },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-market-baseline]').text()).toContain('$1.250')
    expect(wrapper.get('[data-channel-quote]').text()).toContain('$0.250')
    expect(wrapper.get('[data-channel-quote]').text()).toContain(
      'Fixture channel'
    )
    expect(wrapper.get('[data-market-telemetry]').text()).toContain('统计')
    expect(wrapper.text()).not.toMatch(/\d+%|TPS|优惠|最低|最优/)
    await wrapper.get('button[aria-label*="模型详情"]').trigger('click')
    expect(wrapper.emitted('detail')).toEqual([
      ['fixture-model', 'fixture-model:9'],
    ])
    wrapper.unmount()
  })
  it('shows unknown capabilities and unavailable prices without fake zero prices', () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.price_status = 'unavailable'
    fixture.models[0]!.pricing_values = {}
    fixture.offers[0]!.price_status = 'unavailable'
    fixture.offers[0]!.pricing_values = {}
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelCard, {
      props: { model },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('未提供')
    expect(wrapper.text()).not.toContain('$0.000')
    wrapper.unmount()
  })
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
