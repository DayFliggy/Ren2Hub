import type { Observability, ResourceObservation } from '../observability'
import type { PerformanceStats, RoutingSnapshot } from '../opsMonitor'

/** Native monitoring contracts; unavailable host probes are never invented. */
export function performanceFixture(): PerformanceStats {
  return {
    observability: observationFixture(),
    memory_stats: {
      alloc: 1_048_576,
      total_alloc: 8_388_608,
      sys: 4_194_304,
      num_gc: 8,
      num_goroutine: 32,
    },
    cache_stats: {
      active_disk_files: 0,
      current_disk_usage_bytes: 0,
      active_memory_buffers: 2,
      current_memory_usage_bytes: 2048,
      disk_cache_hits: 0,
      memory_cache_hits: 10,
    },
    disk_cache_info: {
      path: '/tmp/cache',
      exists: true,
      file_count: 0,
      total_size: 0,
    },
    disk_space_info: { total: 1000, free: 600, used: 400, used_percent: 40 },
    config: {
      monitor_enabled: true,
      monitor_cpu_threshold: 90,
      monitor_memory_threshold: 90,
      monitor_disk_threshold: 95,
      disk_cache_path: '/tmp/cache',
    },
    // Monitoring has no fresh sample: host CPU and memory are unknown, not 0%.
    system_usage: null,
    db_pool: {
      main: {
        max_open_connections: 0,
        open_connections: 3,
        in_use: 1,
        idle: 2,
        wait_count: 4,
        wait_duration_ms: 1500,
        max_idle_closed: 0,
        max_lifetime_closed: 0,
      },
      log: null,
      log_shared: true,
    },
  }
}

/** C3 wire response: one idle hour, one partially failing hour. */
export function qualityFixture() {
  return {
    enabled: true,
    window_start: 1_792_533_600,
    window_end: 1_792_540_900,
    bucket_seconds: 3600,
    summary: { success_rate: 95, avg_latency_ms: 820, avg_tps: 31.5 } as {
      success_rate: number
      avg_latency_ms: number
      avg_tps: number
    } | null,
    series: [
      {
        ts: 1_792_533_600,
        request_count: 0,
        success_count: 0,
        success_rate: null as number | null,
        avg_latency_ms: null as number | null,
        avg_tps: null as number | null,
      },
      {
        ts: 1_792_537_200,
        request_count: 40,
        success_count: 38,
        success_rate: 95,
        avg_latency_ms: 820,
        avg_tps: 31.5,
      },
    ],
  }
}

export function routingFixture(): RoutingSnapshot {
  return {
    users: [{ id: 1, limit: 4, in_flight: 2 }],
    channels: [{ id: 9, limit: 0, in_flight: 2 }],
    observations: [
      {
        channel_id: 9,
        model: 'fixture-model',
        stream: true,
        samples: 60,
        error_rate: 0.1,
        latency_ms: 1500,
        known: true,
        operation: 1,
        latency_samples: 55,
        latency_known: true,
        updated_at: 1_792_353_600_000,
      },
    ],
  }
}

export function observationFixture(): Observability {
  const unknown = (): ResourceObservation => ({
    available: false,
    percent: null,
    scope: 'unknown',
    sampled_at: null,
    state: 'unknown',
    used_bytes: null,
    total_bytes: null,
    threshold: 90,
  })
  return {
    scope: 'instance',
    instance_started_at: 1,
    sampled_at: 3600,
    stale: false,
    status: 'unknown',
    coverage: { measured: 1, total: 4 },
    checks: [
      { id: 'cpu', status: 'unknown', reason: 'not_sampled' },
      { id: 'memory', status: 'unknown', reason: 'not_sampled' },
      { id: 'disk', status: 'ok', reason: '' },
      { id: 'database', status: 'unknown', reason: 'unavailable' },
      { id: 'redis', status: 'disabled', reason: 'disabled' },
    ],
    resources: {
      cpu: unknown(),
      memory: unknown(),
      disk: {
        available: true,
        percent: 40,
        scope: 'filesystem',
        sampled_at: 3600,
        state: 'ok',
        used_bytes: 400,
        total_bytes: 1000,
        threshold: 95,
      },
    },
    storage: {
      database: { status: 'unknown', latency_ms: null, reason: 'unavailable' },
      log_database: null,
      log_shared: true,
      redis: { status: 'disabled', latency_ms: null, reason: 'disabled' },
    },
    process: {
      goroutines: 32,
      heap_alloc_bytes: 1_048_576,
      heap_sys_bytes: 4_194_304,
      gc_count: 8,
      last_gc_pause_ms: null,
    },
    // Outbound differs from inbound in every sample, so a swapped or renamed
    // field shows as a wrong figure or an empty line.
    traffic: {
      scope: 'host_interface',
      inbound_mbps: 1.5,
      outbound_mbps: 6.2,
      interface: 'eth0',
      reason: '',
    },
    series: {
      window: '1h',
      bucket_seconds: 60,
      points: [0, 1, 2].map((i) => ({
        ts: 3480 + i * 60,
        cpu: null,
        memory: null,
        disk: 40,
        inbound_mbps: 1.3 + i * 0.1,
        outbound_mbps: 6 + i * 0.1,
        goroutines: 32,
        heap_alloc_bytes: 1_048_576,
        user_in_flight: 1,
        channel_in_flight: 2,
      })),
    },
  }
}

export function measuredResource(
  percent: number,
  threshold = 90
): ResourceObservation {
  return {
    available: true,
    percent,
    scope: 'container',
    sampled_at: 3600,
    state: 'ok',
    used_bytes: null,
    total_bytes: null,
    threshold,
  }
}
