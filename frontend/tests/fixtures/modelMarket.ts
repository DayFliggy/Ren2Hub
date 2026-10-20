import type {
  MarketModelContract,
  MarketPricingContract,
  ModelMarketResponse,
} from '../../src/types/modelMarket'

function baseline(
  id: string,
  values: MarketPricingContract['values'],
  endpoints = ['openai'],
  extra: Partial<MarketPricingContract> = {}
): MarketPricingContract {
  return {
    id,
    version: 'fixture-v1',
    configured: true,
    values,
    supported_endpoint_types: endpoints,
    ...extra,
  }
}

function model(
  id: string,
  vendor: string,
  baselines: MarketPricingContract[],
  extra: Partial<MarketModelContract> = {}
): MarketModelContract {
  return {
    id,
    vendor,
    description: `${vendor} model for market contract checks`,
    icon: '',
    capabilities: {
      context_limit: 128000,
      output_limit: 8192,
      tool_call: true,
      input_modalities: ['text'],
      output_modalities: ['text'],
      reasoning_efforts: null,
      source: null,
      updated_at: 0,
    },
    supported_endpoint_types: [
      ...new Set(baselines.flatMap((price) => price.supported_endpoint_types)),
    ],
    baselines,
    ...extra,
  }
}

/** Fixture mirrors the authenticated, decimal-string market contract. */
export function createModelMarketFixture(): ModelMarketResponse {
  const models = [
    model('gpt-4.1', 'OpenAI', [
      baseline('gpt-4.1:default', {
        ModelRatio: '1.25',
        CompletionRatio: '4',
        CacheRatio: '0.25',
        CreateCacheRatio: '1.25',
      }),
    ]),
    model('claude-sonnet-4', 'Anthropic', [
      baseline(
        'claude-sonnet-4:default',
        {
          ModelRatio: '1.5',
          CompletionRatio: '5',
          CacheRatio: '0.1',
          CreateCacheRatio: '0',
        },
        ['anthropic']
      ),
    ]),
    model('gemini-2.5-pro', 'Google', [
      baseline(
        'gemini-2.5-pro:default',
        {
          ModelRatio: '0.625',
          CompletionRatio: '8',
        },
        ['gemini']
      ),
    ]),
    model(
      'audio-preview',
      'OpenAI',
      [
        baseline(
          'audio-preview:default',
          {
            ModelRatio: '2.5',
            CompletionRatio: '4',
            AudioRatio: '8',
            AudioCompletionRatio: '2',
          },
          ['openai-audio']
        ),
      ],
      {
        capabilities: {
          context_limit: null,
          output_limit: null,
          tool_call: false,
          input_modalities: ['audio', 'text'],
          output_modalities: ['audio', 'text'],
          reasoning_efforts: null,
          source: null,
          updated_at: 0,
        },
      }
    ),
    model('image-preview', 'OpenAI', [
      baseline(
        'image-preview:default',
        {
          ModelPrice: '0.04',
        },
        ['image-generation']
      ),
    ]),
    model('video-preview', 'Platform', [
      baseline(
        'video-preview:seconds',
        {
          'billing_setting.billing_mode': 'tiered_expr',
          'billing_setting.billing_expr': 'u("seconds") * 0.2',
        },
        ['openai-video'],
        {
          plugin_key: 'seconds',
          plugin_name: 'Video seconds',
          usage_schema: { seconds: { type: 'number', unit: 'second' } },
        }
      ),
      baseline('video-preview:fixed', { ModelPrice: '0.08' }, ['video-task'], {
        plugin_key: 'fixed',
        plugin_name: 'Fixed video',
      }),
    ]),
    model('unpriced-preview', '', [
      baseline('unpriced-preview:default', {}, ['openai'], {
        configured: false,
      }),
    ]),
  ]
  const channels = [
    { id: 11, name: '雾桥中转', ratio: '0.2' },
    { id: 12, name: '原生专线', ratio: '1' },
    { id: 13, name: '长文专线', ratio: '0.6' },
  ]
  return {
    schema_version: 1,
    models,
    channels,
    supported_endpoints: {
      openai: { path: '/v1/chat/completions', method: 'POST' },
      anthropic: { path: '/v1/messages', method: 'POST' },
      gemini: { path: '/v1beta/models/:model:generateContent', method: 'POST' },
      'openai-audio': { path: '/v1/audio/speech', method: 'POST' },
      'image-generation': { path: '/v1/images/generations', method: 'POST' },
      'openai-video': { path: '/v1/videos', method: 'POST' },
    },
    offers: models.flatMap((entry) =>
      entry.baselines.flatMap((price) =>
        channels
          .filter(
            (channel) => channel.id !== 13 || entry.id === 'claude-sonnet-4'
          )
          .map((channel) => ({
            id: `${entry.id}:${channel.id}:${price.id}`,
            model_id: entry.id,
            channel_id: channel.id,
            pricing_id: price.id,
            supported_endpoint_types: price.supported_endpoint_types,
          }))
      )
    ),
    telemetry: {
      status: 'unavailable',
      reason: 'channel_metrics_not_collected',
    },
  }
}
