/** C2 /api/data/self/summary: counts from quota_data, latency from own logs. */
export function dashboardFixture(marker = 10) {
  return {
    kpi: {
      totalTokens: marker,
      totalQuota: '25',
      totalRequests: 2,
      avgLatency: 820 as number | null,
      successRate: 50 as number | null,
    },
    comparison: { quotaDelta: null, requestsDelta: null },
    models: [
      {
        model: 'fixture-model',
        tokens: marker,
        quota: '25',
        requests: 2,
        share: 1,
        avgLatency: 820 as number | null,
      },
    ],
    hourly: [
      { ts: 1792537200, requests: 0, tokens: 0, quota: 0 },
      { ts: 1792540800, requests: 2, tokens: marker, quota: '25' },
    ],
    flow: [{ date: '2026-10-21', consume: '25', requests: 2, tokens: marker }],
    token_trend: [
      { date: '2026-10-21', total: marker, cache_read: 4, cache_create: 1 },
    ],
    distribution: [
      { date: '2026-10-21', consume: '25', requests: 2, tokens: marker },
    ],
    meta: {
      start: 1792540800,
      end: 1792627200,
      tz_offset: -480,
      generated_at: 1792584000,
      available: true,
      source: 'quota_data',
      bucket_seconds: 3600,
      flush_interval_minutes: 5,
      coverage: { latency: true, success_rate: true },
    },
  }
}
