import { describe, expect, it } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import {
  buildLiveModelCatalog,
  PLATFORM_VENDOR,
  useModelMarket,
} from '@/composables/useModelMarket'
import { parsePricingModels } from '@/api/liveContracts'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
describe('live model market adapter', () => {
  it('uses manifest capabilities and endpoints instead of broader pricing metadata', () => {
    const catalog = buildLiveModelCatalog(
      ['allowed'],
      [],
      [],
      [
        {
          id: 'allowed',
          visible: true,
          supported_endpoint_types: ['embeddings'],
          price_configured: true,
          price_version: 'v1',
          capabilities: {
            reasoning_efforts: null,
            input_modalities: null,
            output_modalities: null,
            context_limit: 32000,
            output_limit: null,
            tool_call: false,
            source: null,
            updated_at: 0,
          },
        },
      ]
    )
    expect(catalog.models[0]).toMatchObject({
      name: 'allowed',
      type: 'embedding',
      context: 32000,
    })
  })
  it('keeps unconfigured and missing model prices out of token billing', () => {
    const catalog = buildLiveModelCatalog(
      ['unpriced', 'missing'],
      parsePricingModels([
        {
          model_name: 'unpriced',
          quota_type: 0,
          billing_mode: 'unconfigured',
          model_ratio: 0,
          completion_ratio: 0,
        },
      ]),
      []
    )
    expect(catalog.models.map((model) => model.billing)).toEqual([
      'unconfigured',
      'unconfigured',
    ])
    expect(catalog.models.map((model) => model.price)).toEqual([{}, {}])
  })
  it('combines pricing and performance while preserving unpriced models', () => {
    const catalog = buildLiveModelCatalog(
      ['chat', 'audio', 'missing'],
      parsePricingModels([
        {
          model_name: 'chat',
          quota_type: 0,
          model_ratio: 0.5,
          completion_ratio: 1.2,
          cache_ratio: 0.25,
          create_cache_ratio: 0,
          owner_by: 'OpenAI',
        },
        {
          model_name: 'audio',
          quota_type: 1,
          model_price: 0.0001,
          supported_endpoint_types: ['audio'],
        },
      ]),
      [
        {
          model_name: 'chat',
          avg_latency_ms: 800,
          success_rate: 98,
          avg_tps: 40,
        },
      ]
    )
    expect(catalog.models[0]).toMatchObject({
      billing: 'token',
      price: {
        input: '1',
        output: '1.2',
        cache_read: '0.25',
        cache_write: '0',
      },
      latency: 0.8,
      tps: 40,
      health: 98,
    })
    expect(catalog.models[1]).toMatchObject({
      billing: 'per_call',
      price: { per_call: '0.0001' },
      health: 0,
    })
    expect(catalog.models[2]?.price).toEqual({})
  })
  it('preserves model IDs and vendor names without inventing expression unit prices', () => {
    const names = ['provider/model-openai-compact', 'model-preview']
    const catalog = buildLiveModelCatalog(
      names,
      parsePricingModels(
        names.map((model_name) => ({
          model_name,
          owner_by: 'Moonshot AI',
          quota_type: 0,
          billing_mode: 'tiered_expr',
          billing_expr: 'len > 100 ? tier("long",p * 4) : tier("short",p * 2)',
        }))
      ),
      []
    )
    expect(catalog.models.map((model) => model.name)).toEqual(names)
    expect(catalog.vendors).toEqual(['Moonshot AI'])
    expect(catalog.models[0]?.price).toEqual({})
    expect(catalog.models[0]?.pricing?.billing_expr).toContain('len > 100')
  })
})

describe('model market vendor groups', () => {
  it('keeps ownerless models in one group whose name follows the language', async () => {
    await loadMessageDomain('console')
    await setLocale('en')
    let market!: ReturnType<typeof useModelMarket>
    const wrapper = mount(
      defineComponent({
        setup() {
          market = useModelMarket()
          return () => null
        },
      }),
      { global: { plugins: [i18n] } }
    )
    market.catalog.value = buildLiveModelCatalog(
      ['house-a', 'kimi-k2', 'house-b'],
      parsePricingModels([
        {
          model_name: 'kimi-k2',
          owner_by: 'Moonshot AI',
          quota_type: 0,
          billing_mode: 'unconfigured',
          model_ratio: 0,
          completion_ratio: 0,
        },
      ]),
      []
    )

    expect(
      market.groups.value.map((group) => [group.label, group.models.length])
    ).toEqual([
      ['Platform', 2],
      ['Moonshot AI', 1],
    ])
    expect(market.groups.value[1]?.tagline).toBe(
      'Kimi long-context and agent capabilities'
    )
    market.keyword.value = 'platform'
    expect(market.filtered.value.map((model) => model.name)).toEqual([
      'house-a',
      'house-b',
    ])

    market.keyword.value = ''
    market.vendor.value = PLATFORM_VENDOR
    await setLocale('zh-CN')
    expect(market.groups.value.map((group) => group.label)).toEqual(['平台'])
    wrapper.unmount()
  })
})
