import { offerFixture } from '@/api/__tests__/pricingFixture'
import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import ModelCard from '@/components/console/models/ModelCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { MarketModel } from '@/types/console'
import type { PriceVersion } from '@/api/modelPricing'

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
  it('displays each channel price with its flat fee and graduated bands', () => {
    const versions: PriceVersion[] = [9, 10].map((channel_id) => ({
      id: channel_id,
      model: 'scoped',
      scope_kind: 'channel_model',
      mode: 'unit_price',
      flat_fee: '0.01',
      created_at: 1,
      hash: `hash-${channel_id}`,
      prices: [
        {
          quantity: 'p',
          unit: 'per_million_tokens',
          price: '',
          tier_mode: 'graduated',
          tiers: [{ up_to: '1000000', unit_price: '2' }, { unit_price: '1' }],
        },
      ],
    }))
    const wrapper = mount(ModelCard, {
      props: {
        model: {
          ...tieredModel,
          billing: 'token',
          price: {},
          pricing: versions[0],
          offers: versions.map((price) => offerFixture(price)),
        },
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.findAll('[data-model-price-scope]')).toHaveLength(2)
    expect(wrapper.text()).toContain('channel_model · v9')
    expect(wrapper.text()).toContain('channel_model · v10')
    expect(wrapper.text()).not.toContain('channel_id')
    expect(wrapper.text()).toContain('分段累加')
    expect(wrapper.text()).toContain('$0.010')
    expect(wrapper.text()).toContain('≤1000000: $2.000')
    wrapper.unmount()
  })
  it.each(['grid', 'list'] as const)(
    'retains mixed token, media and call prices in %s layout',
    (layout) => {
      const wrapper = mount(ModelCard, {
        props: {
          layout,
          model: {
            ...tieredModel,
            billing: 'unit_price',
            price: { input: '2', per_call: '0.01' },
            pricing: {
              id: 1,
              model: 'mixed',
              mode: 'unit_price',
              created_at: 1,
              hash: 'hash',
              prices: [
                { quantity: 'p', unit: 'per_million_tokens', price: '2' },
                { quantity: 'calls', unit: 'per_call', price: '0.01' },
                { quantity: 'images', unit: 'per_image', price: '0.1256789' },
              ],
            },
            offers: [
              offerFixture({
                id: 1,
                model: 'mixed',
                mode: 'unit_price',
                created_at: 1,
                hash: 'hash',
                prices: [
                  { quantity: 'p', unit: 'per_million_tokens', price: '2' },
                  { quantity: 'calls', unit: 'per_call', price: '0.01' },
                  { quantity: 'images', unit: 'per_image', price: '0.1256789' },
                ],
              }),
            ],
          },
        },
        global: { plugins: [i18n] },
      })
      expect(wrapper.text()).toContain('$2.000')
      expect(wrapper.text()).toContain('$0.010')
      expect(wrapper.text()).toContain('$0.126')
      expect(wrapper.text()).toContain('每张图片')
      expect(wrapper.get('[data-model-billing]').text()).toBe('明确单价')
      wrapper.unmount()
    }
  )

  it('shows an expression with supplemental prices without inventing token unit prices', () => {
    const wrapper = mount(ModelCard, {
      props: {
        model: {
          ...tieredModel,
          price: {},
          pricing: {
            id: 1,
            model: 'expression',
            mode: 'expression',
            created_at: 1,
            hash: 'hash',
            expression: 'p * 0.00123456789',
            prices: [{ quantity: 'seconds', unit: 'per_second', price: '0.1' }],
          },
          offers: [
            offerFixture({
              id: 1,
              model: 'expression',
              mode: 'expression',
              created_at: 1,
              hash: 'hash',
              expression: 'p * 0.00123456789',
              prices: [
                { quantity: 'seconds', unit: 'per_second', price: '0.1' },
              ],
            }),
          ],
        },
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('pre').text()).toBe('p * 0.00123456789')
    expect(wrapper.text()).toContain('每秒')
    expect(wrapper.text()).toContain('$0.100')
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
