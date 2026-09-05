import { describe, expect, it } from 'vitest'

import { catalogPrices, parseRankings } from '@/api/publicCatalog'

describe('public catalog contracts', () => {
  it('renders token and per-call price calculations with the selected group ratio', () => {
    const prices = catalogPrices(
      {
        model_name: 'demo',
        description: '',
        icon: '',
        tags: '',
        vendor_id: 1,
        quota_type: 0,
        model_ratio: 2,
        model_price: 0,
        owner_by: 'Demo',
        completion_ratio: 3,
        cache_ratio: 0.5,
        create_cache_ratio: null,
        enable_groups: ['default'],
        supported_endpoint_types: [],
        billing_mode: '',
        billing_expr: '',
        image_ratio: null,
        audio_ratio: null,
        audio_completion_ratio: null,
      },
      1.5,
      'M'
    )
    expect(prices).toEqual([
      { key: 'input', value: 6 },
      { key: 'output', value: 18 },
      { key: 'cacheRead', value: 3 },
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
