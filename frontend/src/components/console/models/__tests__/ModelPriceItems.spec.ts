import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import { parsePricingModels } from '@/api/liveContracts'
import ModelPriceItems from '../ModelPriceItems.vue'

describe('ModelPriceItems', () => {
  it('shows a small 1K token price without rounding it to zero', () => {
    const [pricing] = parsePricingModels([
      {
        model_name: 'test-model',
        quota_type: 0,
        billing_mode: 'ratio',
        model_ratio: 0.05,
      },
    ])
    const wrapper = mount(ModelPriceItems, {
      props: { pricing, unit: 'K' },
      global: {
        plugins: [
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
      },
    })
    expect(wrapper.text()).toContain('<$0.001 / 1K tokens')
    expect(wrapper.text()).not.toContain('$0.000 / 1K tokens')
    wrapper.unmount()
  })
})
