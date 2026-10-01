/** Contract-shaped ops monitor payloads for Playwright `page.route` mocks. */
const NOW = Math.floor(new Date('2026-07-27T12:00:00+08:00').getTime() / 1000)
const BUCKET = 20

function wave(i: number, base: number, amplitude: number, period = 24) {
  return Math.round((base + amplitude * Math.sin(i / period)) * 10) / 10
}

/** 1h window, 20 s buckets; the process started 50 minutes ago and one bucket is missing. */
function seriesPoints() {
  return Array.from({ length: 150 }, (_, i) => i)
    .filter((i) => i !== 90)
    .map((i) => ({
      ts: NOW - (149 - i) * BUCKET,
      cpu: wave(i, 34, 9),
      memory: wave(i, 58, 3, 40),
      disk: 71.4,
      net_up_mbps: wave(i, 1.4, 0.6),
      net_down_mbps: wave(i, 4.2, 1.8),
      goroutines: Math.round(wave(i, 210, 25)),
      heap_alloc_bytes: Math.round(wave(i, 182_000_000, 20_000_000)),
      in_flight: Math.max(0, Math.round(wave(i, 9, 6, 12))),
      queued: i > 120 && i < 128 ? 3 : 0,
      requests_per_minute: wave(i, 220, 70, 18),
      success_rate: i === 0 ? null : wave(i, 99.2, 0.6),
      granted_per_minute: wave(i, 218, 70, 18),
      rejected_per_minute: i > 120 && i < 128 ? 4 : 0,
    }))
}

export function opsOverview(window: '1h' | '6h' | '24h' = '1h') {
  return {
    schema_version: 1,
    sampled_at: NOW,
    status: 'warning',
    checks: [
      { key: 'cpu', status: 'ok', value: 36.2, threshold: 90 },
      { key: 'memory', status: 'ok', value: 58.9, threshold: 90 },
      { key: 'disk', status: 'warning', value: 71.4, threshold: 80 },
      { key: 'success_rate', status: 'ok', value: 99.4 },
      { key: 'admission', status: 'ok', value: 0 },
      { key: 'draining', status: 'ok' },
      { key: 'master', status: 'ok' },
      { key: 'database', status: 'ok', value: 0.8 },
      { key: 'log_database', status: 'ok', value: 1.3 },
      { key: 'redis', status: 'ok', value: 0.3 },
    ],
    runtime: {
      version: 'v2.6.0',
      go_version: 'go1.26.1',
      goos: 'linux',
      goarch: 'amd64',
      node_name: 'ren2hub-primary',
      started_at: NOW - 3000,
      uptime_seconds: 3000,
      master: true,
      draining: false,
    },
    resources: {
      monitor_enabled: true,
      cpu: { percent: 36.2, threshold: 90 },
      memory: {
        percent: 58.9,
        used_bytes: 4_830_000_000,
        total_bytes: 8_201_000_000,
        threshold: 90,
      },
      disk: {
        percent: 71.4,
        used_bytes: 76_700_000_000,
        total_bytes: 107_400_000_000,
        threshold: 80,
        path: '/data',
      },
      network: { up_mbps: 1.6, down_mbps: 4.9 },
      process: {
        goroutines: 214,
        heap_alloc_bytes: 186_000_000,
        heap_sys_bytes: 262_000_000,
        gc_count: 1840,
        last_gc_pause_ms: 0.42,
      },
    },
    gateway: {
      admission: {
        in_flight: 12,
        queued: 0,
        total_queue_size: 1000,
        pools: 6,
        granted_total: 184_220,
        released_total: 184_208,
        draining: false,
        queue_full_total: 3,
        total_queue_full_total: 0,
        wait_timeout_total: 11,
        canceled_total: 4,
        wait_duration_ms_buckets: [412, 690, 822, 901, 934, 940, 951, 953],
      },
      wait_bucket_bounds_ms: [1, 5, 20, 100, 500, 1000, 5000, null],
      retry_budget: {
        enabled: true,
        available_hundredths: 1840,
        reserved: 1,
        first_sends_total: 184_220,
        retry_sends_total: 2_310,
        rejected_total: 12,
      },
      user_queue: {
        active: 0,
        entered_total: 640,
        completed_total: 631,
        failed_total: 9,
        wait_ms_total: 48_200,
      },
      channel_queue: {
        active: 0,
        entered_total: 313,
        completed_total: 307,
        failed_total: 6,
        wait_ms_total: 21_900,
      },
      success_rate: 99.4,
      granted_per_minute: 231,
      rejected_per_minute: 0,
    },
    storage: {
      database: {
        kind: 'postgres',
        status: 'ok',
        latency_ms: 0.8,
        error: '',
        open: 18,
        in_use: 4,
        idle: 14,
        max_open: 100,
        wait_count: 2,
      },
      log_database: {
        kind: 'mysql',
        status: 'ok',
        latency_ms: 1.3,
        error: '',
        open: 6,
        in_use: 1,
        idle: 5,
        max_open: 0,
        wait_count: 0,
      },
      redis: {
        enabled: true,
        status: 'ok',
        latency_ms: 0.3,
        error: '',
        total_conns: 12,
        idle_conns: 10,
        stale_conns: 0,
      },
    },
    series: { window, bucket_seconds: BUCKET, points: seriesPoints() },
  }
}

export function opsRequests() {
  const hourly = Array.from({ length: 24 }, (_, i) => {
    const requests = i < 3 ? 0 : Math.round(wave(i, 9000, 3500, 4))
    const successRate =
      requests === 0 ? null : i === 14 ? 72.5 : i === 15 ? 41.8 : 99.1
    return {
      ts: NOW - (23 - i) * 3600,
      requests,
      success_rate: successRate,
      avg_latency_ms: requests === 0 ? 0 : 640 + i * 12,
      avg_tps: requests === 0 ? 0 : 48.5,
    }
  })
  return {
    enabled: true,
    hours: 24,
    bucket_seconds: 3600,
    summary: {
      requests: hourly.reduce((sum, bucket) => sum + bucket.requests, 0),
      success_rate: 96.8,
      avg_latency_ms: 812,
      avg_tps: 47.3,
    },
    series: hourly,
    top_models: [
      ['gpt-4o', 61_220, 99.6, 910],
      ['claude-sonnet-4-5', 38_410, 98.9, 1_420],
      ['gemini-2.5-pro', 21_090, 97.2, 1_880],
      ['gpt-4o-mini', 18_300, 99.9, 420],
      ['deepseek-chat', 9_870, 92.4, 2_310],
      ['text-embedding-3-large', 4_120, 100, 120],
    ].map(([model_name, requests, success_rate, avg_latency_ms]) => ({
      model_name,
      requests,
      success_rate,
      avg_latency_ms,
      avg_tps: 46.1,
    })),
  }
}

export function opsChannels(unhealthy = 3) {
  return {
    channels: {
      total: 48,
      enabled: 44,
      manually_disabled: 3,
      auto_disabled: 1,
    },
    breakers: { closed: 120, open: Math.max(2, unhealthy - 1), half_open: 1 },
    unhealthy: Array.from({ length: unhealthy }, (_, i) => ({
      channel_id: 30 + i,
      channel_name:
        i === 0
          ? 'openai-primary-with-a-very-long-channel-name'
          : `upstream-${i}`,
      model: i % 2 ? 'claude-sonnet-4-5' : 'gpt-4o',
      state: i === 1 ? 'half_open' : 'open',
      failure_count: 5 + i,
      cooldown_until: NOW + 120 + i * 30,
      last_latency_ms: 12_000 + i * 100,
      first_token_latency_ms: i === 0 ? null : 900,
      updated_at: NOW - i * 15,
    })),
  }
}

export function opsTaskSummary() {
  const row = (
    type: string,
    status: string,
    extra: Record<string, unknown> = {}
  ) => ({
    type,
    scheduled: true,
    enabled: true,
    interval_seconds: 600,
    last_success_at: NOW - 240,
    last_failure_at: null,
    last_error: '',
    active: false,
    status,
    ...extra,
  })
  return {
    types: [
      row('async_task_poll', 'inactive', {
        enabled: false,
        interval_seconds: 15,
      }),
      row('billing_recovery', 'ok', { interval_seconds: 60 }),
      row('channel_test', 'ok'),
      row('gateway_state_cleanup', 'failing', {
        last_failure_at: NOW - 60,
        last_error: 'gateway state lock lost while compacting expired bindings',
      }),
      row('log_cleanup', 'idle', {
        scheduled: false,
        interval_seconds: 0,
        last_success_at: null,
      }),
      row('midjourney_poll', 'inactive', {
        enabled: false,
        interval_seconds: 15,
      }),
      row('model_update', 'stale', {
        interval_seconds: 3600,
        last_success_at: NOW - 9000,
      }),
      row('route_capability_refresh', 'running', { active: true }),
    ],
  }
}

export function opsBillingMetrics() {
  return {
    schema_version: 2,
    recovery_scope: 'database',
    billing_recovery: { pending: 1, oldest_updated_at: NOW - 340 },
    billing: {
      counter_scope: 'instance',
      started_at: NOW - 3000,
      source_refresh_failures_total: {},
      candidate_rejections_total: {},
      missing_price_total: 0,
      calculation_total: 184_100,
      calculation_failures_total: 0,
      settlements_total: {
        confirmed: 180_000,
        estimated: 3100,
        frozen_estimate: 1000,
      },
    },
  }
}

export const ACTIVE_TASKS = [
  {
    task_id: 'route-refresh-1',
    type: 'route_capability_refresh',
    status: 'running',
    created_at: NOW - 600,
    updated_at: NOW - 15,
    locked_by: 'ren2hub-primary',
    state: { progress: 52, processed: 52, total: 100 },
  },
]
