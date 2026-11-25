import type {
  ModelCatalog,
  CatalogModel,
  CatalogPrices,
} from '../../src/api/modelCatalog'

/** Native shared model catalog: server prices already include each channel multiplier. */
export function createModelMarketFixture(): ModelCatalog {
  const model = (
    model_name: string,
    vendor_id: number,
    pricing_values: CatalogPrices,
    endpoints = ['openai']
  ): CatalogModel => ({
    model_name,
    vendor_id,
    description: 'Native model catalog fixture',
    icon: '',
    supported_endpoint_types: endpoints,
    price_status: Object.keys(pricing_values).length
      ? 'configured'
      : 'unavailable',
    pricing_values,
  })
  const models = [
    model('gpt-4.1', 1, { input: '2.5', output: '10' }),
    model('claude-sonnet-4', 2, { input: '3', output: '15' }, ['anthropic']),
    model('gemini-2.5-pro', 3, { input: '1.25', output: '10' }, ['gemini']),
    model('audio-preview', 1, { input: '40', output: '80' }, ['openai-audio']),
    model('image-preview', 1, { request: '0.04' }, ['image-generation']),
    model('video-preview', 0, { request: '0.08' }, ['openai-video']),
    model('unpriced-preview', 0, {}),
  ]
  const rawChannels = [
    { id: 11, name: '雾桥中转', ratio: '0.2' },
    { id: 12, name: '原生专线', ratio: '1' },
    { id: 13, name: '长文专线', ratio: '0.6' },
  ]
  const offers = models.flatMap((entry) =>
    rawChannels
      .filter(
        (channel) => channel.id !== 13 || entry.model_name === 'claude-sonnet-4'
      )
      .map((channel) => ({
        model_id: entry.model_name,
        channel_id: channel.id,
        channel_ratio: channel.ratio,
        supported_endpoint_types: entry.supported_endpoint_types,
        price_status: entry.price_status,
        pricing_values: Object.fromEntries(
          Object.entries(entry.pricing_values).map(([key, value]) => [
            key,
            String(Number((Number(value) * Number(channel.ratio)).toFixed(8))),
          ])
        ),
        health:
          channel.id === 11 && entry.model_name === 'gpt-4.1'
            ? ({
                availability: 'no_restriction',
                quality: 'no_downgrade',
              } as const)
            : channel.id === 11 && entry.model_name === 'claude-sonnet-4'
              ? ({ availability: 'paused', quality: 'unrated' } as const)
              : channel.id === 12 && entry.model_name === 'gpt-4.1'
                ? ({ availability: 'limited', quality: 'unrated' } as const)
                : ({
                    availability: 'no_restriction',
                    quality: 'unrated',
                  } as const),
      }))
  )
  const channels = rawChannels.map(({ ratio, ...row }) => {
    const chOffers = offers.filter((o) => o.channel_id === row.id)
    const affected = chOffers.filter(
      (o) =>
        o.health.availability !== 'no_restriction' ||
        o.health.quality === 'degraded' ||
        o.health.quality === 'severely_degraded'
    ).length
    const evaluated = chOffers.filter(
      (o) =>
        o.health.availability === 'no_restriction' &&
        o.health.quality === 'no_downgrade'
    ).length
    return {
      ...row,
      type: 1,
      status: 1,
      channel_ratio: ratio,
      max_concurrency: 8,
      eligible: true,
      filter_reason: '',
      lab: { id: 'mixed', name: 'Mixed', source: 'mapping' },
      health: {
        visible_models: chOffers.length,
        affected_models: affected,
        evaluated_models: evaluated,
        unrated_models: chOffers.length - affected - evaluated,
      },
    }
  })
  return {
    schema_version: 1,
    models,
    channels,
    vendors: [
      { id: 1, name: 'OpenAI' },
      { id: 2, name: 'Anthropic' },
      { id: 3, name: 'Google' },
    ],
    server_address: 'https://api.example.test',
    generated_at: 1785124800,
    supported_endpoints: {
      openai: { path: '/v1/chat/completions', method: 'POST' },
      anthropic: { path: '/v1/messages', method: 'POST' },
      gemini: { path: '/v1beta/models/:model:generateContent', method: 'POST' },
      'openai-audio': { path: '/v1/audio/speech', method: 'POST' },
      'image-generation': { path: '/v1/images/generations', method: 'POST' },
      'openai-video': { path: '/v1/videos', method: 'POST' },
    },
    offers,
  }
}
