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

it('shows first-tier conditions and example facts without presenting the example as a unit price', () => {
  const [pricing] = parsePricingModels([
    {
      model_name: 'task',
      quota_type: 0,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("HD", u("seconds") * 0.2)',
      billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
      usage_reference: {
        status: 'available',
        amount_usd: '1',
        label: 'five seconds',
        plugin_name: 'Provider A',
        facts: { seconds: '5', audio: false },
        matched_tier: 'HD',
      },
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
  expect(wrapper.text()).toContain('Example reference')
  expect(wrapper.text()).toContain('Provider A')
  expect(wrapper.text()).toContain('five seconds')
  expect(wrapper.text()).toContain('/ this example')
  expect(wrapper.text()).toContain('Conditions: HD')
  expect(wrapper.find('[aria-label="Example usage"]').text()).toContain(
    'seconds5'
  )
  expect(wrapper.text()).not.toContain('/ request')
  wrapper.unmount()
})

it('shows each task variant total and a single model token price in mixed mode', () => {
  const [pricing] = parsePricingModels([
    {
      model_name: 'mixed',
      quota_type: 0,
      billing_mode: 'ratio',
      model_ratio: 0.5,
      completion_ratio: 3,
      pricing_values: { ModelRatio: '0.5', CompletionRatio: '3' },
      billing_plugin_variants: [
        {
          plugin_key: 'a',
          plugin_name: 'Provider A',
          billing_mode: 'tiered_expr',
          billing_expr: 'u("seconds") * 0.1',
          usage_reference: {
            status: 'available',
            amount_usd: '0.7',
            label: 'seven seconds',
            facts: { seconds: '7' },
          },
        },
        {
          plugin_key: 'b',
          plugin_name: 'Provider B',
          billing_mode: 'tiered_expr',
          billing_expr: 'u("tokens") * 0.01',
          usage_reference: {
            status: 'available',
            amount_usd: '0.1',
            label: 'ten tokens',
            facts: { tokens: '10' },
          },
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
  const summary = wrapper.get('.model-price-items > [data-reference-prices]')
  expect(summary.text()).toContain('$1.000')
  expect(summary.text()).toContain('$3.000')
  expect(summary.text()).toContain('Provider A')
  expect(summary.text()).toContain('$0.700')
  expect(summary.text()).not.toContain('Provider B')
  const details = wrapper.findAll('details')
  expect(details).toHaveLength(2)
  expect(details[0]!.get('strong').text()).toBe('$0.700')
  expect(details[0]!.text()).toContain('/ this example')
  expect(details[1]!.get('strong').text()).toBe('$0.100')
  expect(details[1]!.text()).toContain('/ this example')
  expect(wrapper.findAll('[data-price-dimension="input"]')).toHaveLength(1)
  expect(wrapper.findAll('[data-price-dimension="output"]')).toHaveLength(1)
  wrapper.unmount()
})
