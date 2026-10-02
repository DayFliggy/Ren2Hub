import { describe, expect, it } from 'vitest'
import { buildLiveModelCatalog } from '@/composables/useModelMarket'
import { parsePricingModels } from '@/api/liveContracts'
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
