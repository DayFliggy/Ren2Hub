import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import ModelCard from '@/components/console/models/ModelCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { MarketModel } from '@/types/console'
import { parsePricingModels } from '@/api/liveContracts'

const tieredModel: MarketModel = {
  id: 1,
  name: 'gpt-4o',
  vendor: 'OpenAI',
  type: 'chat',
  billing: 'tiered',
  price: {
    input: '2.5',
    output: '10',
    cache_read: '1.25',
    cache_write: '3.125',
    tiers: [
      { label: '0-128K', input: '2.5', output: '10' },
      { label: '128K+', input: '5', output: '15' },
    ],
  },
  context: 128_000,
  tagline: '全能旗舰多模态模型，均衡的速度与质量。',
  latency: 1.78,
  tps: 62.4,
  health: 98,
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

function render(layout: 'grid' | 'list') {
  return mount(ModelCard, {
    props: { model: tieredModel, layout },
    global: { plugins: [i18n] },
  })
}

describe('ModelCard billing metadata', () => {
  it('shows a task provider inheriting the configured model price as available', () => {
    const [pricing] = parsePricingModels([
      {
        model_name: 'shared-task',
        quota_type: 1,
        model_price: 0.2,
        billing_plugin_variants: [
          {
            plugin_key: 'inherited',
            plugin_name: 'Inherited',
            billing_mode: 'ratio',
            billing_expr: '',
          },
        ],
      },
    ])
    const wrapper = mount(ModelCard, {
      props: {
        model: {
          ...tieredModel,
          billing: 'per_call',
          price: { per_call: '0.2' },
          pricing,
        },
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('$0.200')
    expect(wrapper.get('details').text()).toContain('使用上方模型单价')
    expect(wrapper.text()).not.toContain('价格未配置')
    wrapper.unmount()
  })
  it.each(['grid', 'list'] as const)(
    'marks unconfigured model and plugin prices unavailable in %s layout',
    (layout) => {
      const [pricing] = parsePricingModels([
        {
          model_name: 'unpriced',
          quota_type: 0,
          billing_mode: 'unconfigured',
          model_ratio: 0,
          completion_ratio: 0,
          billing_plugin_variants: [
            {
              plugin_key: 'incompatible',
              plugin_name: 'Incompatible',
              billing_mode: 'unconfigured',
              billing_expr: 'u("removed") * 0.4',
            },
            {
              plugin_key: 'active',
              plugin_name: 'Active',
              billing_mode: 'tiered_expr',
              billing_expr: 'u("seconds") * 0.2',
            },
          ],
        },
      ])
      const wrapper = mount(ModelCard, {
        props: {
          model: {
            ...tieredModel,
            billing: 'unconfigured',
            price: {},
            pricing,
          },
          layout,
        },
        global: { plugins: [i18n] },
      })
      expect(wrapper.get('[data-model-billing]').text()).toBe('未配置价格')
      expect(wrapper.text()).toContain('价格未配置')
      expect(wrapper.text()).not.toContain('$0.000')
      expect(wrapper.text()).not.toContain('u("removed")')
      expect(wrapper.text()).toContain('u("seconds") * 0.2')
      wrapper.unmount()
    }
  )
  it('shows an expression without inventing token unit prices', () => {
    const wrapper = mount(ModelCard, {
      props: {
        model: {
          ...tieredModel,
          price: {},
          pricing: {
            model_name: 'gpt-4o',
            description: '',
            icon: '',
            tags: '',
            vendor_id: 0,
            owner_by: 'OpenAI',
            supported_endpoint_types: [],
            quota_type: 0,
            billing_mode: 'tiered_expr',
            billing_expr: 'p * 0.00123456789',
          },
        },
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('pre').text()).toBe('p * 0.00123456789')
    expect(wrapper.text()).not.toContain('$0.000')
    wrapper.unmount()
  })
  it.each(['grid', 'list'] as const)(
    'shows one billing label with its tier count in %s layout',
    (layout) => {
      const wrapper = render(layout)

      expect(wrapper.findAll('[data-model-billing]')).toHaveLength(1)
      expect(wrapper.get('[data-model-billing]').text()).toBe('分档动态计费')
      expect(wrapper.get('[data-model-tier-count]').text()).toBe('2 档')
    }
  )

  it('keeps context out of the grid card while preserving price rows', () => {
    const wrapper = render('grid')

    expect(wrapper.text()).not.toContain('128K')
    expect(wrapper.text()).toContain('$2.500')
    expect(wrapper.text()).toContain('$10.000')
    expect(wrapper.text()).toContain('$1.250')
    expect(wrapper.get('[data-model-cache-read]').text()).toContain('$1.250')
    expect(wrapper.get('[data-model-cache-write]').text()).toContain('$3.125')
    expect(wrapper.get('[data-model-divider]').classes()).toContain(
      'border-[var(--border-default)]'
    )
    expect(wrapper.find('[data-model-channels]').exists()).toBe(false)
  })

  it('omits an unavailable cache-write row without hiding cache read', () => {
    const wrapper = mount(ModelCard, {
      props: {
        model: {
          ...tieredModel,
          price: { ...tieredModel.price, cache_write: undefined },
        },
        layout: 'grid',
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.find('[data-model-cache-read]').exists()).toBe(true)
    expect(wrapper.find('[data-model-cache-write]').exists()).toBe(false)
  })

  it('renders a zero cache-write price as a real price', () => {
    const wrapper = mount(ModelCard, {
      props: {
        model: {
          ...tieredModel,
          price: { ...tieredModel.price, cache_write: '0' },
        },
        layout: 'grid',
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.get('[data-model-cache-write]').text()).toContain('$0.000')
  })

  it('keeps cache pricing out of the compact list layout', () => {
    const wrapper = render('list')

    expect(wrapper.find('[data-model-cache-prices]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('缓存读取')
    expect(wrapper.text()).not.toContain('缓存写入')
  })

  it('uses semantic surfaces across day and night themes', async () => {
    document.documentElement.dataset.theme = 'light'
    const wrapper = render('grid')
    const card = wrapper.get('[data-model-card]')
    const pricePanel = wrapper.get('[data-model-price-panel]')

    expect(card.classes()).toContain('bg-[var(--surface-solid)]')
    expect(pricePanel.classes()).toContain('bg-[var(--surface-table-header)]')
    expect(card.attributes('class')).not.toMatch(/bg-(?:white|black)/)

    document.documentElement.dataset.theme = 'dark'
    await wrapper.vm.$nextTick()
    expect(card.classes()).toContain('border-[var(--border-subtle)]')
    expect(pricePanel.classes()).toContain('border-[var(--border-default)]')
  })

  it('hides the tier count for non-tiered pricing', () => {
    const wrapper = mount(ModelCard, {
      props: {
        model: {
          ...tieredModel,
          billing: 'token',
          price: { input: '0.15', output: '0.6' },
        },
        layout: 'grid',
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.findAll('[data-model-billing]')).toHaveLength(1)
    expect(wrapper.get('[data-model-billing]').text()).toBe('按量计费')
    expect(wrapper.find('[data-model-tier-count]').exists()).toBe(false)
  })
})
