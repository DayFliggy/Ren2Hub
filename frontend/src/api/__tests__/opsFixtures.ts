/** Contract-shaped `/api/ops/*` and task-summary payloads shared by tests. */
export function overviewFixture() {
  return {
    schema_version: 1,
    sampled_at: 1_767_225_600,
    status: 'warning',
    health_score: 85,
    checks: [
      { key: 'cpu', status: 'ok', value: 12.5, threshold: 90 },
      { key: 'redis', status: 'critical', error: 'timeout' },
      { key: 'master', status: 'warning' },
    ],
    runtime: {
      version: 'v1.2.3',
      go_version: 'go1.26.1',
      goos: 'linux',
      goarch: 'amd64',
      node_name: 'ren2hub',
      started_at: 1_767_220_000,
      uptime_seconds: 5600,
      master: false,
      draining: false,
    },
    resources: {
      monitor_enabled: true,
      cpu: { percent: null, threshold: 90 },
      memory: { percent: 40.1, used_bytes: 1, total_bytes: 2, threshold: 90 },
      disk: {
        percent: 51,
        used_bytes: 1,
        total_bytes: 2,
        threshold: 95,
        path: '/tmp',
      },
      network: { up_mbps: null, down_mbps: 1.1 },
      process: {
        goroutines: 120,
        heap_alloc_bytes: 1,
        heap_sys_bytes: 2,
        gc_count: 30,
        last_gc_pause_ms: 0.4,
      },
    },
    gateway: {
      admission: {
        in_flight: 3,
        queued: 0,
        total_queue_size: 1000,
        pools: 4,
        granted_total: 10,
        released_total: 7,
        draining: false,
        queue_full_total: 0,
        total_queue_full_total: 0,
        wait_timeout_total: 0,
        canceled_total: 0,
        wait_duration_ms_buckets: [1, 2, 2, 3, 3, 3, 3, 4],
      },
      wait_bucket_bounds_ms: [1, 5, 20, 100, 500, 1000, 5000, null],
      retry_budget: {
        enabled: true,
        available_hundredths: 100,
        reserved: 0,
        first_sends_total: 10,
        retry_sends_total: 1,
        rejected_total: 0,
      },
      user_queue: {
        active: 0,
        entered_total: 0,
        completed_total: 0,
        failed_total: 0,
        wait_ms_total: 0,
      },
      channel_queue: {
        active: 0,
        entered_total: 0,
        completed_total: 0,
        failed_total: 0,
        wait_ms_total: 0,
      },
      success_rate: null,
      granted_per_minute: null,
      rejected_per_minute: 0,
    },
    storage: {
      database: {
        kind: 'postgres',
        status: 'ok',
        latency_ms: 0.8,
        error: '',
        open: 5,
        in_use: 1,
        idle: 4,
        max_open: 1000,
        wait_count: 0,
      },
      log_database: null,
      redis: {
        enabled: true,
        status: 'critical',
        latency_ms: null,
        error: 'timeout',
        total_conns: 10,
        idle_conns: 9,
        stale_conns: 0,
      },
    },
    series: {
      window: '1h',
      bucket_seconds: 20,
      points: [
        {
          ts: 1_767_225_580,
          cpu: 12,
          memory: 40,
          disk: 51,
          net_up_mbps: null,
          net_down_mbps: 1.1,
          goroutines: 118,
          heap_alloc_bytes: 1,
          in_flight: 2,
          queued: 0,
          requests_per_minute: 0,
          success_rate: null,
          granted_per_minute: 4,
          rejected_per_minute: 0,
        },
      ],
    },
  }
}

const idleTraffic = {
  requests: 0,
  success_rate: null,
  avg_latency_ms: 0,
  avg_tps: 0,
}

export function requestsFixture() {
  return {
    enabled: true,
    hours: 24,
    bucket_seconds: 3600,
    summary: {
      requests: 50,
      success_rate: 98,
      avg_latency_ms: 820,
      avg_tps: 45.2,
    },
    series: [
      { ts: 1_767_222_000, ...idleTraffic },
      {
        ts: 1_767_225_600,
        requests: 50,
        success_rate: 98,
        avg_latency_ms: 820,
        avg_tps: 45.2,
      },
    ],
    top_models: [
      {
        model_name: 'gpt-4o',
        requests: 50,
        success_rate: 98,
        avg_latency_ms: 820,
        avg_tps: 45.2,
      },
    ],
  }
}

export function channelsFixture() {
  return {
    channels: {
      total: 12,
      enabled: 10,
      manually_disabled: 1,
      auto_disabled: 1,
    },
    breakers: { closed: 30, open: 1, half_open: 1 },
    unhealthy: [
      {
        channel_id: 3,
        channel_name: 'openai-a',
        model: 'gpt-4o',
        state: 'open',
        failure_count: 5,
        cooldown_until: 1_767_225_900,
        last_latency_ms: 12_000,
        first_token_latency_ms: null,
        updated_at: 1_767_225_600,
      },
    ],
  }
}

export function billingFixture() {
  return {
    schema_version: 2,
    recovery_scope: 'database',
    billing_recovery: { pending: 2, oldest_updated_at: 1_767_225_000 },
    billing: {
      counter_scope: 'instance',
      started_at: 1_767_220_000,
      calculation_total: 123,
      calculation_failures_total: 1,
      missing_price_total: 0,
      settlements_total: { confirmed: 115, estimated: 5, frozen_estimate: 2 },
    },
  }
}
