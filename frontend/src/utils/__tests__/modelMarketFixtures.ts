import { parsePricingCatalog } from '@/api/publicCatalog'
import { buildModelMarketCatalog } from '@/utils/modelMarket'

export function nativeMarketFixture() {
  const pricing = parsePricingCatalog({
    success: true,
    supported_endpoint: {
      openai: { path: '/v1/chat/completions', method: 'POST' },
      'openai-video': { path: '/v1/videos', method: 'POST' },
    },
    vendors: [{ id: 1, name: 'Provider One', description: '' }],
    data: [
      {
        model_name: 'chat-one',
        vendor_id: 1,
        description: 'A long model description',
        tags: 'tools,reasoning',
        supported_endpoint_types: ['openai'],
        quota_type: 0,
        model_ratio: 0.5,
        completion_ratio: 3,
        cache_ratio: 0.1,
      },
      {
        model_name: 'chat-two',
        vendor_id: 1,
        supported_endpoint_types: ['openai'],
        quota_type: 0,
        model_ratio: 2,
        completion_ratio: 1,
      },
      {
        model_name: 'movie',
        supported_endpoint_types: ['openai-video'],
        quota_type: 0,
        billing_mode: 'tiered_expr',
        billing_expr: 'u("seconds") * 0.2',
        billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
        billing_plugin_variants: [
          {
            plugin_key: 'provider-video',
            plugin_name: 'Provider Video',
            billing_mode: 'tiered_expr',
            billing_expr: 'u("seconds") * 0.3',
          },
        ],
      },
      { model_name: 'fixed', quota_type: 1, model_price: 0.25 },
      {
        model_name: 'free',
        quota_type: 0,
        model_ratio: 0,
        billing_mode: 'ratio',
      },
      {
        model_name: 'unconfigured',
        quota_type: 0,
        model_ratio: 0,
        billing_mode: 'unconfigured',
      },
      { model_name: 'private-only', quota_type: 0, model_ratio: 99 },
      { model_name: 'broken' },
    ],
  })
  const groups = [
    {
      name: 'standard',
      description: 'Standard',
      ratio: 0.2,
      models: [
        'chat-one',
        'movie',
        'fixed',
        'free',
        'unconfigured',
        'broken',
        'missing',
      ],
    },
    {
      name: 'premium',
      description: 'Premium',
      ratio: 2,
      models: ['chat-one', 'chat-two'],
    },
    {
      name: 'auto',
      description: 'Automatic',
      ratio: 'auto' as const,
      models: ['chat-one', 'chat-two', 'movie'],
    },
  ]
  return { pricing, groups, catalog: buildModelMarketCatalog(pricing, groups) }
}
