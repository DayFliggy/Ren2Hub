import type { ModelCatalog } from '../modelCatalog'
export function modelCatalogFixture(): ModelCatalog {
  return {
    schema_version: 1,
    models: [
      {
        model_name: 'fixture-model',
        description: 'Fixture',
        icon: '',
        vendor_id: -10,
        supported_endpoint_types: ['openai', 'openai-response'],
        price_status: 'configured',
        pricing_values: { input: '1.25', output: '5' },
        capabilities: null,
      },
    ],
    vendors: [{ id: -10, name: 'Fixture vendor' }],
    supported_endpoints: {
      openai: { path: '/v1/chat/completions', method: 'POST' },
      'openai-response': { path: '/v1/responses', method: 'POST' },
    },
    channels: [
      {
        id: 9,
        name: 'Fixture channel',
        type: 1,
        status: 1,
        channel_ratio: '0.2',
        max_concurrency: 3,
        in_flight: 1,
        eligible: true,
        filter_reason: '',
        lab: { id: 'fixture', name: 'Fixture lab', source: 'vendor' },
      },
    ],
    offers: [
      {
        model_id: 'fixture-model',
        channel_id: 9,
        supported_endpoint_types: ['openai', 'openai-response'],
        channel_ratio: '0.2',
        price_status: 'configured',
        pricing_values: { input: '0.25', output: '1' },
      },
    ],
    server_address: 'https://api.example.test',
    generated_at: 1792281600,
  }
}
export const nativeTokenFixture = {
  id: 1,
  name: 'Fixture key',
  key: 'sk-***fixture',
  type: 'auto',
  status: 1,
  used_quota: 0,
  remain_quota: 0,
  unlimited_quota: true,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: null,
  expired_time: -1,
  created_time: 1,
}
