import type { ModelCatalog } from '../../src/api/modelCatalog'

export function nativeModelCatalog(now: number): ModelCatalog {
  const prices = [
    ['gpt-native', 'OpenAI', '2.5', '10'],
    ['gpt-4.1', 'OpenAI', '2.5', '10'],
    ['claude-sonnet-4', 'Anthropic', '3', '15'],
    ['gemini-2.5-pro', 'Google', '1.25', '10'],
  ] as const
  const channels = [
    { id: 2, name: 'Native channel', ratio: '1' },
    { id: 11, name: '雾桥中转', ratio: '0.2' },
    { id: 12, name: '原生专线', ratio: '1' },
    { id: 13, name: '长文专线', ratio: '0.6' },
  ]
  return {
    schema_version: 1,
    generated_at: now,
    server_address: 'https://api.example.test',
    vendors: [
      { id: 1, name: 'OpenAI' },
      { id: 2, name: 'Anthropic' },
      { id: 3, name: 'Google' },
    ],
    supported_endpoints: {
      openai: { path: '/v1/chat/completions', method: 'POST' },
      'openai-response': { path: '/v1/responses', method: 'POST' },
    },
    models: prices.map(([name, vendor, input, output]) => ({
      model_name: name,
      description: `${vendor} fixture model`,
      icon: '',
      vendor_id: vendor === 'OpenAI' ? 1 : vendor === 'Anthropic' ? 2 : 3,
      supported_endpoint_types: ['openai', 'openai-response'],
      price_status: 'configured',
      pricing_values: { input, output },
      capabilities: null,
    })),
    channels: channels.map(({ id, name, ratio }) => ({
      id,
      name,
      type: 1,
      status: 1,
      channel_ratio: ratio,
      max_concurrency: 8,
      in_flight: 2,
      eligible: true,
      filter_reason: '',
      lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
    })),
    offers: prices.flatMap(([model_id, , input, output]) =>
      channels.map((channel) => ({
        model_id,
        channel_id: channel.id,
        channel_ratio: channel.ratio,
        supported_endpoint_types: ['openai', 'openai-response'],
        price_status: 'configured' as const,
        pricing_values: {
          input: String(Number(input) * Number(channel.ratio)),
          output: String(Number(output) * Number(channel.ratio)),
        },
      }))
    ),
  }
}

export function nativeUsageSummary(now: number, includeComparison = true) {
  const lastHour = (Math.ceil(now / 3600) - 1) * 3600
  const distribution = Array.from({ length: 366 }, (_, index) => ({
    date: new Date((now - (365 - index) * 86400) * 1000)
      .toISOString()
      .slice(0, 10),
    requests: 40 + ((index * 17) % 180),
    consume: 12000 + ((index * 7919) % 180000),
    tokens: 8000 + ((index * 12401) % 900000),
  }))
  return {
    kpi: {
      totalTokens: 1200,
      totalQuota: 1000000,
      totalRequests: 4,
      avgLatency: 1270,
      successRate: null,
    },
    comparison: includeComparison
      ? { quotaDelta: 8.4, requestsDelta: 5.1 }
      : null,
    models: [
      {
        model: 'gpt-native',
        tokens: 1200,
        quota: 1000000,
        requests: 4,
        share: 100,
        avgLatency: 1270,
      },
    ],
    hourly: Array.from({ length: 24 }, (_, index) => ({
      ts: lastHour - (23 - index) * 3600,
      requests: index % 5,
      tokens: (index % 5) * 300,
      quota: (index % 5) * 250000,
    })),
    flow: distribution.slice(-30).map((row) => ({
      date: row.date,
      consume: row.consume,
      requests: row.requests,
    })),
    distribution,
    token_trend: distribution.slice(-30).map((row) => ({
      date: row.date,
      total: row.tokens,
      input: Math.floor(row.tokens * 0.75),
      output: row.tokens - Math.floor(row.tokens * 0.75),
      cache_create: 0,
      cache_read: Math.floor(row.tokens / 4),
      hit_rate: null,
    })),
    meta: {
      start: now - 30 * 86400,
      end: now,
      tz_offset: -480,
      generated_at: now,
      available: true,
      // C2 contract: counts come from quota_data hour buckets.
      source: 'quota_data',
      bucket_seconds: 3600,
      flush_interval_minutes: 5,
      coverage: { latency: true, success_rate: false },
    },
  }
}

export function nativeAffiliateInfo() {
  return {
    code: 'NATIVE-AFF',
    code_enabled: true,
    invited: 2,
    effective_rate_bps: 2000,
    effective_rate_percent: 20,
    available_reward: 1000000,
    frozen_reward: 500000,
    total_reward: 1500000,
    reward_total: 1500000,
    transferable: 1000000,
    records: [],
    monthly_series: [],
    policy: {
      enabled: true,
      rate_bps: 2000,
      freeze_hours: 0,
      duration_days: 0,
      per_invitee_cap_quota: 0,
      admin_recharge_enabled: false,
    },
  }
}
