import type { Observability, ResourceObservation } from '@/api/observability'
const gib = 1024 ** 3
export function resourceFixture(
  changes: Partial<ResourceObservation> = {}
): ResourceObservation {
  return {
    available: true,
    percent: 34,
    scope: 'container',
    sampled_at: 1_798_540_000,
    state: 'ok',
    used_bytes: null,
    total_bytes: null,
    threshold: 90,
    ...changes,
  }
}
export function observabilityFixture(): Observability {
  return {
    scope: 'instance',
    instance_started_at: 1_798_500_000,
    sampled_at: 1_798_540_000,
    stale: false,
    status: 'ok',
    coverage: { measured: 5, total: 5 },
    checks: [],
    resources: {
      cpu: resourceFixture(),
      memory: resourceFixture({
        percent: 32.5,
        used_bytes: 5.2 * gib,
        total_bytes: 16 * gib,
      }),
      disk: resourceFixture({
        percent: 42.6,
        scope: 'filesystem',
        used_bytes: 218 * gib,
        total_bytes: 512 * gib,
      }),
    },
    storage: {
      database: { status: 'ok', latency_ms: 1, reason: '' },
      log_database: null,
      log_shared: true,
      redis: { status: 'disabled', latency_ms: null, reason: 'disabled' },
    },
    process: {
      goroutines: 50,
      heap_alloc_bytes: gib,
      heap_sys_bytes: 2 * gib,
      gc_count: 0,
      last_gc_pause_ms: null,
    },
    traffic: {
      scope: 'application_http_body',
      request_mbps: 2.1,
      response_mbps: 12.4,
    },
    series: {
      window: '1h',
      bucket_seconds: 60,
      points: [0, 1, 2].map((i) => ({
        ts: 1_798_539_880 + i * 60,
        cpu: 34,
        memory: 32.5,
        disk: 42.6,
        request_mbps: 1.1 + i * 0.5,
        response_mbps: 8.6 + i * 1.9,
        goroutines: 50,
        heap_alloc_bytes: gib,
      })),
    },
  }
}
