import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import ModelPriceMatrix from '../ModelPriceMatrix.vue'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
beforeAll(() => setLocale('zh-CN'))
describe('native catalog comparison', () => {
  it('keeps absent channel supply blank and aligns the real offer with its baseline', () => {
    const fixture = modelCatalogFixture()
    fixture.channels.push({
      ...fixture.channels[0]!,
      id: 10,
      name: 'No supply',
    })
    const catalog = buildModelMarketCatalog(fixture)
    const wrapper = mount(ModelPriceMatrix, {
      props: { models: catalog.models, channels: catalog.channels },
      global: { plugins: [i18n] },
    })
    const row = wrapper.get('tbody tr')
    expect(row.get('[data-channel-id="10"]').text()).toBe('—')
    expect(row.get('[data-channel-id="9"]').text()).toContain('$0.250')
    expect(row.get('.benchmark-column').text()).toContain('$1.250')
    expect(wrapper.findAll('button dl')).toHaveLength(0)
    wrapper.unmount()
  })
})
