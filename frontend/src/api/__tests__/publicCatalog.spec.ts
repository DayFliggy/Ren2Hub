import { offerFixture } from './pricingFixture'
import { describe, expect, it } from 'vitest'

import {
  catalogPrices,
  parsePricingCatalog,
  parseRankings,
} from '@/api/publicCatalog'

describe('public catalog contracts', () => {
  it('returns a partial directory with explicit unavailable records', () => {
    const offer = offerFixture({
      id: 1,
      model: 'priced-model',
      mode: 'unit_price',
      prices: [{ quantity: 'ai_cr', unit: 'per_million_tokens', price: '0.4' }],
      created_at: 1,
      hash: 'audio-cache',
    })
    const result = parsePricingCatalog({
      success: true,
      supported_endpoint: {},
      data: [
        { model_name: 'request-alias', offers: [offer] },
        { model_name: 'unavailable', offers: [] },
      ],
    })
    expect(result.models.map((model) => model.model_name)).toEqual([
      'request-alias',
    ])
    expect(result.issues).toEqual([
      {
        model_name: 'unavailable',
        offer_id: null,
        reason: 'invalid_model_record',
      },
    ])
    expect(catalogPrices(result.models[0]!, 'M')).toEqual([
      { key: 'audioCacheRead', value: '0.4', unit: 'per_million_tokens' },
    ])
  })

  it('renders version prices without mutable channel multipliers', () => {
    const prices = catalogPrices(
      {
        model_name: 'demo',
        description: '',
        icon: '',
        tags: '',
        vendor_id: 1,
        owner_by: 'Demo',
        supported_endpoint_types: [],
        offers: [
          offerFixture({
            id: 1,
            model: 'demo',
            mode: 'unit_price' as const,
            prices: [
              { quantity: 'p', unit: 'per_million_tokens', price: '4' },
              { quantity: 'c', unit: 'per_million_tokens', price: '12' },
              { quantity: 'cr', unit: 'per_million_tokens', price: '2' },
            ],
            created_at: 1,
            hash: 'fixture',
          }),
        ],
      },
      'M'
    )
    expect(prices).toEqual([
      { key: 'input', value: '4', unit: 'per_million_tokens' },
      { key: 'output', value: '12', unit: 'per_million_tokens' },
      { key: 'cacheRead', value: '2', unit: 'per_million_tokens' },
    ])
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
