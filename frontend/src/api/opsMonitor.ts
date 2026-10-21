import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
export interface RoutingObservation {
  channel_id: number
  model: string
  stream: boolean
  samples: number
  error_rate: number
  latency_ms: number
  known: boolean
  /** Unix milliseconds, as supplied by the routing observer. */
  updated_at: number
  operation: number | null
  latency_samples: number | null
  latency_known: boolean
}
export interface RoutingSnapshot {
  users: { id: number; limit: number; in_flight: number }[]
  channels: { id: number; limit: number; in_flight: number }[]
  observations: RoutingObservation[]
}
/** Counters of common.DiskCacheStats; an absent counter stays null, not 0. */
export interface DiskCacheStats {
  current_memory_usage_bytes: number | null
  current_disk_usage_bytes: number | null
  active_memory_buffers: number | null
  active_disk_files: number | null
  memory_cache_hits: number | null
  disk_cache_hits: number | null
}
export interface PerformanceStats {
  memory_stats: {
    alloc: number
    total_alloc: number
    sys: number
    num_gc: number
    num_goroutine: number
  }
  cache_stats: DiskCacheStats
  disk_cache_info: Record<string, unknown>
  disk_space_info: Record<string, unknown>
  config: Record<string, unknown>
}
export function parseRoutingSnapshot(value: unknown): RoutingSnapshot {
  const endpoint = '/api/performance/routing'
  if (!isRecord(value)) invalidResponse(endpoint)
  const rows = (key: string) => {
    const result = value[key]
    if (!Array.isArray(result) || result.some((row) => !isRecord(row)))
      invalidResponse(endpoint)
    return result as Record<string, unknown>[]
  }
  const limits = (key: string) =>
    rows(key).map((row) => ({
      id: nonnegative(row.id, endpoint, true),
      limit: nonnegative(row.limit, endpoint, true),
      in_flight: nonnegative(row.in_flight, endpoint, true),
    }))
  return {
    users: limits('users'),
    channels: limits('channels'),
    observations: rows('observations').map((row) => {
      const samples = nonnegative(row.samples, endpoint, true)
      const latencySamples = optionalNumber(row.latency_samples, endpoint, true)
      const known = requiredBoolean(row.known, endpoint)
      const latencyKnown =
        row.latency_known === undefined
          ? false
          : requiredBoolean(row.latency_known, endpoint)
      return {
        channel_id: requiredStrictInteger(row.channel_id, endpoint),
        model: requiredString(row.model, endpoint),
        stream: requiredBoolean(row.stream, endpoint),
        samples,
        error_rate: probability(row.error_rate, endpoint),
        latency_ms: nonnegative(row.latency_ms, endpoint),
        known: known && samples > 0,
        updated_at: nonnegative(row.updated_at, endpoint, true),
        operation: optionalNumber(row.operation, endpoint, true),
        latency_samples: latencySamples,
        latency_known:
          latencyKnown && latencySamples !== null && latencySamples > 0,
      }
    }),
  }
}
export function parsePerformanceStats(value: unknown): PerformanceStats {
  const endpoint = '/api/performance/stats'
  if (
    !isRecord(value) ||
    !isRecord(value.memory_stats) ||
    !isRecord(value.cache_stats) ||
    !isRecord(value.disk_cache_info) ||
    !isRecord(value.disk_space_info) ||
    !isRecord(value.config)
  )
    invalidResponse(endpoint)
  const memory = value.memory_stats
  const rawCache = value.cache_stats
  const counter = (key: keyof DiskCacheStats) =>
    optionalNumber(rawCache[key], endpoint)
  // Validate the native optional metrics at the boundary, so malformed data
  // follows the feed's error path instead of throwing during rendering.
  const cacheStats: DiskCacheStats = {
    current_memory_usage_bytes: counter('current_memory_usage_bytes'),
    current_disk_usage_bytes: counter('current_disk_usage_bytes'),
    active_memory_buffers: counter('active_memory_buffers'),
    active_disk_files: counter('active_disk_files'),
    memory_cache_hits: counter('memory_cache_hits'),
    disk_cache_hits: counter('disk_cache_hits'),
  }
  for (const key of ['total', 'used', 'used_percent']) {
    const metric = optionalNumber(value.disk_space_info[key], endpoint)
    if (key === 'used_percent' && metric !== null && metric > 100)
      invalidResponse(endpoint)
  }
  for (const key of [
    'monitor_cpu_threshold',
    'monitor_memory_threshold',
    'monitor_disk_threshold',
  ]) {
    const threshold = optionalNumber(value.config[key], endpoint)
    if (threshold !== null && threshold > 100) invalidResponse(endpoint)
  }
  performanceBoolean(value.config, 'monitor_enabled')
  const path = value.config.disk_cache_path
  if (path !== undefined && path !== null && typeof path !== 'string')
    invalidResponse(endpoint)
  return {
    memory_stats: {
      alloc: nonnegative(memory.alloc, endpoint),
      total_alloc: nonnegative(memory.total_alloc, endpoint),
      sys: nonnegative(memory.sys, endpoint),
      num_gc: nonnegative(memory.num_gc, endpoint, true),
      num_goroutine: nonnegative(memory.num_goroutine, endpoint, true),
    },
    cache_stats: cacheStats,
    disk_cache_info: value.disk_cache_info,
    disk_space_info: value.disk_space_info,
    config: value.config,
  }
}
export type PerformanceAction = 'gc' | 'disk_cache' | 'reset_stats'
export const opsMonitorApi = {
  execute: (action: PerformanceAction) =>
    action === 'disk_cache'
      ? api.delete('/api/performance/disk_cache')
      : api.post(`/api/performance/${action}`),
  stats: async (signal?: AbortSignal) =>
    parsePerformanceStats(
      await api.get('/api/performance/stats', undefined, { signal })
    ),
  routing: async (signal?: AbortSignal) =>
    parseRoutingSnapshot(
      await api.get('/api/performance/routing', undefined, { signal })
    ),
}

export const OPS_WINDOWS = ['1h', '6h', '24h'] as const
export type OpsWindow = (typeof OPS_WINDOWS)[number]
export const OPS_WINDOW_SECONDS: Record<OpsWindow, number> = {
  '1h': 3600,
  '6h': 21600,
  '24h': 86400,
}
export type OpsStatus = 'ok' | 'warning' | 'critical' | 'unknown'
export interface OpsCheck {
  key: 'cpu' | 'memory' | 'disk' | 'database' | 'redis'
  status: OpsStatus
  value: number | null
  threshold: number | null
}
export interface OpsSeriesPoint {
  ts: number
  cpu: null
  memory: null
  disk: number | null
  users: number | null
  channels: number | null
}
export type OpsSeriesField = Exclude<keyof OpsSeriesPoint, 'ts'>
export interface OpsOverview {
  sampled_at: number
  health_score: null
  status: OpsStatus
  checks: OpsCheck[]
  gateway: RoutingSnapshot | null
  resources: {
    monitor_enabled: boolean | null
    cpu: { percent: null; threshold: number | null }
    memory: {
      percent: null
      threshold: number | null
      used_bytes: null
      total_bytes: null
    }
    disk: {
      percent: number | null
      threshold: number | null
      used_bytes: number | null
      total_bytes: number | null
      path: string | null
    }
    network: { up_mbps: null; down_mbps: null }
    process: {
      goroutines: number
      heap_alloc_bytes: number
      runtime_sys_bytes: number
      gc_count: number
      last_gc_pause_ms: null
    }
  } | null
  cache: PerformanceStats | null
  series: {
    window: OpsWindow
    bucket_seconds: number
    points: OpsSeriesPoint[]
  }
}

function nonnegative(value: unknown, endpoint: string, integer = false) {
  const parsed = integer
    ? requiredStrictInteger(value, endpoint)
    : requiredStrictNumber(value, endpoint)
  if (parsed < 0) invalidResponse(endpoint)
  return parsed
}
function probability(value: unknown, endpoint: string) {
  const parsed = nonnegative(value, endpoint)
  if (parsed > 1) invalidResponse(endpoint)
  return parsed
}
function optionalNumber(
  value: unknown,
  endpoint: string,
  integer = false
): number | null {
  return value === undefined || value === null
    ? null
    : nonnegative(value, endpoint, integer)
}
export function performanceNumber(
  record: Record<string, unknown>,
  key: string
) {
  return optionalNumber(record[key], '/api/performance/stats')
}
function performanceBoolean(record: Record<string, unknown>, key: string) {
  return record[key] === undefined || record[key] === null
    ? null
    : requiredBoolean(record[key], '/api/performance/stats')
}
export function concurrencyTotal(rows: RoutingSnapshot['channels']): number {
  return rows.reduce((sum, row) => sum + row.in_flight, 0)
}
export function seriesRange(overview: OpsOverview): [number, number] {
  return [
    overview.sampled_at - OPS_WINDOW_SECONDS[overview.series.window],
    overview.sampled_at,
  ]
}
export function checkStatus(
  overview: OpsOverview | null,
  key: OpsCheck['key']
): OpsStatus {
  return (
    overview?.checks.find((check) => check.key === key)?.status ?? 'unknown'
  )
}
/** This projection adapts measured native facts to the historical card layout.
 * Unavailable host probes stay null; Go heap is never relabelled host memory.
 */
export function projectOpsOverview(
  stats: PerformanceStats | null,
  routing: RoutingSnapshot | null,
  window: OpsWindow,
  points: OpsSeriesPoint[],
  sampledAt: number
): OpsOverview | null {
  if (!stats && !routing) return null
  const config = stats?.config ?? {}
  const disk = stats?.disk_space_info ?? {}
  const total = performanceNumber(disk, 'total')
  const used = performanceNumber(disk, 'used')
  const measuredDisk = total !== null && total > 0 && used !== null
  const percent = measuredDisk ? performanceNumber(disk, 'used_percent') : null
  if (percent !== null && percent > 100)
    invalidResponse('/api/performance/stats')
  const monitor = performanceBoolean(config, 'monitor_enabled')
  const diskThreshold = performanceNumber(config, 'monitor_disk_threshold')
  const diskStatus: OpsStatus =
    percent === null || monitor !== true || !diskThreshold
      ? 'unknown'
      : percent >= diskThreshold
        ? 'critical'
        : percent >= diskThreshold * 0.9
          ? 'warning'
          : 'ok'
  const checks: OpsCheck[] = [
    {
      key: 'cpu',
      status: 'unknown',
      value: null,
      threshold: performanceNumber(config, 'monitor_cpu_threshold'),
    },
    {
      key: 'memory',
      status: 'unknown',
      value: null,
      threshold: performanceNumber(config, 'monitor_memory_threshold'),
    },
    {
      key: 'disk',
      status: diskStatus,
      value: percent,
      threshold: diskThreshold,
    },
    { key: 'database', status: 'unknown', value: null, threshold: null },
    { key: 'redis', status: 'unknown', value: null, threshold: null },
  ]
  const path = config.disk_cache_path
  if (path !== undefined && path !== null && typeof path !== 'string')
    invalidResponse('/api/performance/stats')
  return {
    sampled_at: sampledAt,
    health_score: null,
    // An unmeasured check prevents an "all healthy" roll-up.
    status: checks.some((check) => check.status === 'critical')
      ? 'critical'
      : checks.some((check) => check.status === 'warning')
        ? 'warning'
        : 'unknown',
    checks,
    gateway: routing,
    cache: stats,
    resources: stats
      ? {
          monitor_enabled: monitor,
          cpu: { percent: null, threshold: checks[0]!.threshold },
          memory: {
            percent: null,
            threshold: checks[1]!.threshold,
            used_bytes: null,
            total_bytes: null,
          },
          disk: {
            percent,
            threshold: diskThreshold,
            used_bytes: measuredDisk ? used : null,
            total_bytes: measuredDisk ? total : null,
            path: typeof path === 'string' ? path : null,
          },
          network: { up_mbps: null, down_mbps: null },
          process: {
            goroutines: stats.memory_stats.num_goroutine,
            heap_alloc_bytes: stats.memory_stats.alloc,
            runtime_sys_bytes: stats.memory_stats.sys,
            gc_count: stats.memory_stats.num_gc,
            last_gc_pause_ms: null,
          },
        }
      : null,
    series: {
      window,
      bucket_seconds: 30,
      points: points.filter(
        (point) => point.ts >= sampledAt - OPS_WINDOW_SECONDS[window]
      ),
    },
  }
}
/** A feed records measured values or an explicit failed-poll gap. Retain by
 * time, so manual refreshes do not shorten the visible 24-hour history.
 */
export function appendOpsSample(
  points: OpsSeriesPoint[],
  point: OpsSeriesPoint
): OpsSeriesPoint[] {
  return [
    ...points.filter(
      (previous) => previous.ts !== point.ts && previous.ts >= point.ts - 86400
    ),
    point,
  ]
}
