import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import ModelPriceMatrix from '../ModelPriceMatrix.vue'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import { createModelMarketFixture } from '../../../../../tests/fixtures/modelMarket'

beforeAll(() => setLocale('zh-CN'))

describe('market comparison variant matching', () => {
  it('keeps a channel missing the first variant aligned with its matching baseline', () => {
    const fixture = createModelMarketFixture()
    fixture.offers = fixture.offers.filter(
      (offer) =>
        !(
          offer.model_id === 'video-preview' &&
          offer.channel_id === 11 &&
          offer.pricing_id === 'video-preview:seconds'
        )
    )
    const catalog = buildModelMarketCatalog(fixture)
    const model = catalog.models.find((entry) => entry.id === 'video-preview')!
    const wrapper = mount(ModelPriceMatrix, {
      props: { models: [model], channels: catalog.channels },
      global: { plugins: [i18n] },
    })
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.get('.model-column').attributes('rowspan')).toBe('2')
    expect(rows[0]!.get('[data-channel-id="11"]').text()).toBe('—')
    expect(rows[0]!.get('.benchmark-column').text()).toContain('Video seconds')
    const fixed = rows[1]!.get('[data-channel-id="11"]')
    expect(fixed.text()).toContain('Fixed video')
    expect(fixed.text()).toContain('$0.016')
    expect(rows[1]!.get('.benchmark-column').text()).toContain('$0.080')
    expect(rows[1]!.find('.model-column').exists()).toBe(false)
    expect(wrapper.findAll('button dl')).toHaveLength(0)
    wrapper.unmount()
  })

  it('retains an explicit unconfigured baseline when a model has no variants', () => {
    const catalog = buildModelMarketCatalog(createModelMarketFixture())
    const model = { ...catalog.models[0]!, baselines: [], offers: [] }
    const wrapper = mount(ModelPriceMatrix, {
      props: { models: [model], channels: catalog.channels },
      global: { plugins: [i18n] },
    })
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.get('tbody .benchmark-column').text()).toContain(
      '价格未配置'
    )
    expect(wrapper.findAll('tbody [data-offer-id]')).toHaveLength(0)
    wrapper.unmount()
  })
})
