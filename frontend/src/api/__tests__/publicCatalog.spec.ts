import { describe, expect, it } from 'vitest'

import {
  catalogPrices,
  parsePricingCatalog,
  parseRankings,
} from '@/api/publicCatalog'

describe('public catalog contracts', () => {
  it('distinguishes an unconfigured backend zero from an explicitly free price', () => {
    const catalog = parsePricingCatalog({
      success: true,
      supported_endpoint: {},
      data: [
        {
          model_name: 'unpriced',
          quota_type: 0,
          billing_mode: 'unconfigured',
          model_ratio: 0,
          model_price: 0,
          completion_ratio: 0,
        },
        {
          model_name: 'free',
          quota_type: 0,
          billing_mode: 'ratio',
          model_ratio: 0,
          completion_ratio: 1,
        },
      ],
    })
    expect(catalog.issues).toEqual([])
    expect(catalogPrices(catalog.models[0]!, 'M')).toEqual([])
    expect(catalogPrices(catalog.models[1]!, 'M')[0]).toEqual({
      key: 'input',
      value: '0',
      unit: 'per_million_tokens',
    })
  })
  it('reports malformed model prices separately and preserves cache dimensions', () => {
    const parsed = parsePricingCatalog({
      success: true,
      supported_endpoint: {},
      data: [
        {
          model_name: 'model',
          quota_type: 0,
          model_ratio: 2,
          completion_ratio: 3,
          cache_ratio: 0,
          create_cache_ratio: 0.5,
        },
        { model_name: 'missing' },
      ],
    })
    expect(parsed.models).toHaveLength(1)
    expect(parsed.issues[0]?.model_name).toBe('missing')
    expect(catalogPrices(parsed.models[0]!, 'M')).toEqual([
      { key: 'input', value: '4', unit: 'per_million_tokens' },
      { key: 'output', value: '12', unit: 'per_million_tokens' },
      { key: 'cacheRead', value: '0', unit: 'per_million_tokens' },
      { key: 'cacheWrite', value: '2', unit: 'per_million_tokens' },
    ])
    expect(catalogPrices(parsed.models[0]!, 'K')[0]?.value).toBe('0.004')
  })
  it('rejects malformed ranking shares and accepts valid history', () => {
    const parsed = parseRankings({
      models: [
        {
          rank: 1,
          model_name: 'demo',
          vendor: 'Demo',
          category: 'all',
          total_tokens: 4,
          share: 1,
          growth_pct: 0,
        },
      ],
      vendors: [
        {
          rank: 1,
          vendor: 'Demo',
          total_tokens: 4,
          share: 1,
          growth_pct: 0,
          models_count: 1,
          top_model: 'demo',
        },
      ],
      top_movers: [],
      top_droppers: [],
      models_history: {
        points: [
          { ts: '1', label: 'now', model: 'demo', vendor: 'Demo', tokens: 4 },
        ],
      },
      vendor_share_history: {
        points: [
          { ts: '1', label: 'now', vendor: 'Demo', share: 1, tokens: 4 },
        ],
      },
    })
    expect(parsed.models[0]?.model_name).toBe('demo')
    expect(() =>
      parseRankings({
        models: [],
        vendors: [],
        top_movers: [],
        top_droppers: [],
        models_history: { points: [] },
        vendor_share_history: {
          points: [
            { ts: '1', label: 'now', vendor: 'Demo', share: 2, tokens: 1 },
          ],
        },
      })
    ).toThrow()
  })
})
