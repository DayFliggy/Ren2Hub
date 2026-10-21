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

it('displays native task formulas without inventing sample amounts', () => {
  const [pricing] = parsePricingModels([
    {
      model_name: 'task',
      quota_type: 0,
      billing_mode: 'tiered_expr',
      billing_expr: 'u("seconds") * 0.2',
      billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
      billing_plugin_variants: [
        {
          plugin_key: 'a',
          plugin_name: 'Provider A',
          billing_mode: 'tiered_expr',
          billing_expr: 'u("seconds") * 0.1',
        },
      ],
    },
  ])
  const wrapper = mount(ModelPriceItems, {
    props: { pricing },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
    },
  })
  expect(wrapper.text()).toContain('u("seconds") * 0.2')
  expect(wrapper.get('details').text()).toContain('Provider A')
  expect(wrapper.get('details').text()).toContain('u("seconds") * 0.1')
  expect(wrapper.text()).not.toContain('$')
  wrapper.unmount()
})

it('renders global numeric prices without a business group multiplier', () => {
  const [pricing] = parsePricingModels([
    {
      model_name: 'native',
      quota_type: 0,
      model_ratio: 0.5,
      completion_ratio: 3,
    },
  ])
  const wrapper = mount(ModelPriceItems, {
    props: { pricing },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
    },
  })
  expect(wrapper.text()).toContain('$1.000')
  expect(wrapper.text()).toContain('$3.000')
  expect(wrapper.findAll('dl')).toHaveLength(1)
  expect(pricing!.model_ratio).toBe(0.5)
  wrapper.unmount()
})
