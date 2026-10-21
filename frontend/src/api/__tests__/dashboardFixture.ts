export function dashboardFixture(marker = 10) {
  return {
    kpi: {
      totalTokens: marker,
      totalQuota: 25,
      totalRequests: 2,
      avgLatency: null,
      successRate: null,
    },
    comparison: { quotaDelta: null, requestsDelta: null },
    models: [
      {
        model: 'fixture-model',
        tokens: marker,
        quota: 25,
        requests: 2,
        share: 1,
        avgLatency: null,
      },
    ],
    hourly: [],
    flow: [{ date: '2026-10-21', consume: 25, requests: 2 }],
    token_trend: [
      {
        date: '2026-10-21',
        total: marker,
        input: null,
        output: null,
        cache_create: null,
        cache_read: null,
        hit_rate: null,
      },
    ],
    distribution: [
      { date: '2026-10-21', consume: 25, requests: 2, tokens: marker },
    ],
    meta: {
      start: 1792540800,
      end: 1792627200,
      tz_offset: -480,
      generated_at: 1792584000,
      available: true,
      source: 'consume_logs',
      cache_coverage: {
        records: 2,
        read_records: 0,
        write_records: 0,
        invalid_records: 0,
      },
      coverage: {
        consume_logs: true,
        cache_usage: false,
        latency: true,
        success_rate: false,
      },
    },
  }
}
