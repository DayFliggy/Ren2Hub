import type { PerformanceStats, RoutingSnapshot } from '../opsMonitor'

/** Native monitoring contracts; unavailable host probes are never invented. */
export function performanceFixture(): PerformanceStats {
  return {
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
