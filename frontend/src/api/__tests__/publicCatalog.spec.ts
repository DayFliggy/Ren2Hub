import { describe, expect, it } from 'vitest'

import { catalogPrices, parseRankings } from '@/api/publicCatalog'

describe('public catalog contracts', () => {
  it('renders token and per-call prices with the channel ratio', () => {
    const prices = catalogPrices(
      {
        model_name: 'demo',
        description: '',
        icon: '',
        tags: '',
        vendor_id: 1,
        owner_by: 'Demo',
        channel_ratio_min: 1.5,
        channel_ratio_max: 2,
        supported_endpoint_types: [],
        billing_mode: '',
        price_version: {
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
        },
      },
      1.5,
      'M'
    )
    expect(prices).toEqual([
      { key: 'input', value: '6', unit: 'per_million_tokens' },
      { key: 'output', value: '18', unit: 'per_million_tokens' },
      { key: 'cacheRead', value: '3', unit: 'per_million_tokens' },
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
