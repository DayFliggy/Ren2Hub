import {
  parseObservability,
  type Observability,
  type ObservationStatus,
  type CheckID,
  type ObservationReason,
} from './observability'
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
export interface RouteBreakerSnapshot {
  channel_id: number
  channel_name?: string
  model: string
  state: 'open' | 'half_open'
  open_until: number
  probe_in_flight: boolean
  trigger: string
  last_status?: number
  last_error_code?: string
  opened_at: number
  reopens: number
}
export interface RouteCooldownSnapshot {
  channel_id: number
  channel_name?: string
  until: number
  source: string
}
export interface RoutingSnapshot {
  breakers?: RouteBreakerSnapshot[]
  cooldowns?: RouteCooldownSnapshot[]
  quality?: {
    channel_id: number
    model: string
    actual_model?: string
    operation: number
    stream: boolean
    band: number
    reason: string
    samples: number
    success_rate?: number
    latency_samples: number
    latency_ms?: number
    speed_samples: number
    tokens_per_second?: number
    updated_at?: number
    evaluated_at?: number
  }[]
  cache?: {
    explorations: number
    free_requests: number
    cells?: number
    cell_limit?: number
    visit_count?: number
    visit_limit?: number
    evictions?: number
    evaluated_at?: number
    evaluation_ms?: number
    bands: {
      channel_id: number
      model: string
      mode: string
      operation: number
      stream: boolean
      known: boolean
      band: number
      reason: string
      coverage: number
      token_rate: number
      samples: number
      sessions: number
    }[]
  }
  users: { id: number; name?: string; limit: number; in_flight: number }[]
  channels: { id: number; name?: string; limit: number; in_flight: number }[]
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
/** Monitor sample shared with the overload sentinel; null when monitoring is
 * off, not yet sampled or older than 30 seconds — never a fabricated 0%. */
export interface SystemUsage {
  /** May exceed 100 during a burst; visuals cap the arc, text keeps the value. */
  cpu_percent: number
  memory_percent: number
  sampled_at: number
}
/** sql.DB.Stats() of one pool; null when the pool could not be read. */
export interface DBPoolStat {
  max_open_connections: number
  open_connections: number
  in_use: number
  idle: number
  wait_count: number
  wait_duration_ms: number
  max_idle_closed: number
  max_lifetime_closed: number
}
export interface DBPoolStats {
  main: DBPoolStat | null
  log: DBPoolStat | null
  /** True when the log database uses the main pool; `log` is then null. */
  log_shared: boolean
}
export interface PerformanceStats {
  observability: Observability
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
  system_usage: SystemUsage | null
  db_pool: DBPoolStats
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
  const optionalString = (raw: unknown): string | undefined => {
    if (raw === undefined || raw === null) return undefined
    return requiredString(raw, endpoint)
  }
  const limits = (key: string) =>
    rows(key).map((row) => ({
      id: nonnegative(row.id, endpoint, true),
      name: optionalString(row.name),
      limit: nonnegative(row.limit, endpoint, true),
      in_flight: nonnegative(row.in_flight, endpoint, true),
    }))
  const breakers = (value.breakers === undefined ? [] : rows('breakers')).map(
    (row) => {
      const state = requiredString(row.state, endpoint)
      if (state !== 'open' && state !== 'half_open') invalidResponse(endpoint)
      return {
        channel_id: requiredStrictInteger(row.channel_id, endpoint),
        channel_name: optionalString(row.channel_name),
        model: requiredString(row.model, endpoint),
        state: state as 'open' | 'half_open',
        open_until: nonnegative(row.open_until, endpoint, true),
        probe_in_flight: requiredBoolean(row.probe_in_flight, endpoint),
        trigger: requiredString(row.trigger, endpoint),
        last_status:
          row.last_status === undefined
            ? undefined
            : nonnegative(row.last_status, endpoint, true),
        last_error_code: optionalString(row.last_error_code),
        opened_at: nonnegative(row.opened_at, endpoint, true),
        reopens: nonnegative(row.reopens, endpoint, true),
      }
    }
  )
  const cooldowns = (
    value.cooldowns === undefined ? [] : rows('cooldowns')
  ).map((row) => ({
    channel_id: requiredStrictInteger(row.channel_id, endpoint),
    channel_name: optionalString(row.channel_name),
    until: nonnegative(row.until, endpoint, true),
    source: requiredString(row.source, endpoint),
  }))
  return {
    breakers,
    cooldowns,
    quality: (value.quality === undefined ? [] : rows('quality')).map(
      (row) => ({
        channel_id: requiredStrictInteger(row.channel_id, endpoint),
        model: requiredString(row.model, endpoint),
        actual_model: optionalString(row.actual_model),
        operation: requiredStrictInteger(row.operation, endpoint),
        stream: requiredBoolean(row.stream, endpoint),
        band: nonnegative(row.band, endpoint, true),
        reason: requiredString(row.reason, endpoint),
        samples: nonnegative(row.samples, endpoint, true),
        success_rate:
          row.success_rate === undefined
            ? undefined
            : probability(row.success_rate, endpoint),
        latency_samples: nonnegative(row.latency_samples, endpoint, true),
        latency_ms:
          row.latency_ms === undefined
            ? undefined
            : nonnegative(row.latency_ms, endpoint),
        speed_samples: nonnegative(row.speed_samples, endpoint, true),
        tokens_per_second:
          row.tokens_per_second === undefined
            ? undefined
            : nonnegative(row.tokens_per_second, endpoint),
        updated_at:
          row.updated_at === undefined
            ? undefined
            : nonnegative(row.updated_at, endpoint, true),
        evaluated_at:
          row.evaluated_at === undefined
            ? undefined
            : nonnegative(row.evaluated_at, endpoint, true),
      })
    ),
    cache: parseRouteCache(value.cache, endpoint),
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
function parseRouteCache(
  value: unknown,
  endpoint: string
): RoutingSnapshot['cache'] {
  if (value === undefined) return undefined
  if (!isRecord(value) || !Array.isArray(value.bands)) invalidResponse(endpoint)
  return {
    explorations: nonnegative(value.explorations, endpoint, true),
    free_requests: nonnegative(value.free_requests, endpoint, true),
    cells:
      value.cells === undefined
        ? undefined
        : nonnegative(value.cells, endpoint, true),
    cell_limit:
      value.cell_limit === undefined
        ? undefined
        : nonnegative(value.cell_limit, endpoint, true),
    visit_count:
      value.visit_count === undefined
        ? undefined
        : nonnegative(value.visit_count, endpoint, true),
    visit_limit:
      value.visit_limit === undefined
        ? undefined
        : nonnegative(value.visit_limit, endpoint, true),
    evictions:
      value.evictions === undefined
        ? undefined
        : nonnegative(value.evictions, endpoint, true),
    evaluated_at:
      value.evaluated_at === undefined
        ? undefined
        : nonnegative(value.evaluated_at, endpoint, true),
    evaluation_ms:
      value.evaluation_ms === undefined
        ? undefined
        : nonnegative(value.evaluation_ms, endpoint, true),
    bands: value.bands.map((row: unknown) => {
      if (!isRecord(row)) invalidResponse(endpoint)
      return {
        channel_id: requiredStrictInteger(row.channel_id, endpoint),
        model: requiredString(row.model, endpoint),
        mode: requiredString(row.mode, endpoint),
        operation: requiredStrictInteger(row.operation, endpoint),
        stream: requiredBoolean(row.stream, endpoint),
        known: requiredBoolean(row.known, endpoint),
        band: nonnegative(row.band, endpoint, true),
        reason: requiredString(row.reason, endpoint),
        coverage: probability(row.coverage, endpoint),
        token_rate: probability(row.token_rate, endpoint),
        samples: nonnegative(row.samples, endpoint, true),
        sessions: nonnegative(row.sessions, endpoint, true),
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
    !isRecord(value.config) ||
    !isRecord(value.db_pool)
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
  ])
    monitorThreshold(value.config, key)
  performanceBoolean(value.config, 'monitor_enabled')
  const path = value.config.disk_cache_path
  if (path !== undefined && path !== null && typeof path !== 'string')
    invalidResponse(endpoint)
  return {
    observability: parseObservability(value.observability),
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
    system_usage: parseSystemUsage(value.system_usage, endpoint),
    db_pool: {
      main: parseDBPool(value.db_pool.main, endpoint),
      log: parseDBPool(value.db_pool.log, endpoint),
      log_shared: requiredBoolean(value.db_pool.log_shared, endpoint),
    },
  }
}
function percentValue(value: unknown, endpoint: string) {
  const parsed = nonnegative(value, endpoint)
  if (parsed > 100) invalidResponse(endpoint)
  return parsed
}
function parseSystemUsage(value: unknown, endpoint: string) {
  if (value === null) return null
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    // Container CPU is cgroup usage over the core quota and is not clamped,
    // so a burst may legitimately read above 100%.
    cpu_percent: nonnegative(value.cpu_percent, endpoint),
    memory_percent: percentValue(value.memory_percent, endpoint),
    sampled_at: nonnegative(value.sampled_at, endpoint, true),
  }
}
function parseDBPool(value: unknown, endpoint: string): DBPoolStat | null {
  if (value === null) return null
  if (!isRecord(value)) invalidResponse(endpoint)
  const count = (key: keyof DBPoolStat) =>
    nonnegative(value[key], endpoint, true)
  return {
    max_open_connections: count('max_open_connections'),
    open_connections: count('open_connections'),
    in_use: count('in_use'),
    idle: count('idle'),
    wait_count: count('wait_count'),
    wait_duration_ms: count('wait_duration_ms'),
    max_idle_closed: count('max_idle_closed'),
    max_lifetime_closed: count('max_lifetime_closed'),
  }
}
/** One hour of perf_metrics summed across models; idle hours keep null rates. */
export interface QualityPoint {
  ts: number
  request_count: number
  success_count: number
  success_rate: number | null
  avg_latency_ms: number | null
  avg_tps: number | null
}
export interface PerformanceQuality {
  enabled: boolean
  window_start: number
  window_end: number
  bucket_seconds: number
  /** Null when the window has no request. */
  summary: {
    requests: number
    success_rate: number
    avg_latency_ms: number
    /** Null when no hour measured output tokens and generation time. */
    avg_tps: number | null
  } | null
  series: QualityPoint[]
}
export function parsePerformanceQuality(value: unknown): PerformanceQuality {
  const endpoint = '/api/performance/quality'
  if (
    !isRecord(value) ||
    !Array.isArray(value.series) ||
    value.series.some((row) => !isRecord(row)) ||
    (value.summary !== null && !isRecord(value.summary))
  )
    invalidResponse(endpoint)
  const rate = (raw: unknown) =>
    raw === null ? null : percentValue(raw, endpoint)
  const series = (value.series as Record<string, unknown>[]).map((row) => {
    const requests = nonnegative(row.request_count, endpoint, true)
    const successes = nonnegative(row.success_count, endpoint, true)
    if (successes > requests) invalidResponse(endpoint)
    return {
      ts: nonnegative(row.ts, endpoint, true),
      request_count: requests,
      success_count: successes,
      success_rate: rate(row.success_rate),
      avg_latency_ms: optionalNumber(row.avg_latency_ms, endpoint),
      avg_tps: optionalNumber(row.avg_tps, endpoint),
    }
  })
  const summary = value.summary
  return {
    enabled: requiredBoolean(value.enabled, endpoint),
    window_start: nonnegative(value.window_start, endpoint, true),
    window_end: nonnegative(value.window_end, endpoint, true),
    bucket_seconds: nonnegative(value.bucket_seconds, endpoint, true),
    summary:
      summary === null
        ? null
        : {
            requests: series.reduce((sum, row) => sum + row.request_count, 0),
            success_rate: percentValue(summary.success_rate, endpoint),
            avg_latency_ms: nonnegative(summary.avg_latency_ms, endpoint),
            // The native summary reports 0 t/s when nothing was measurable.
            avg_tps: series.some((row) => row.avg_tps !== null)
              ? nonnegative(summary.avg_tps, endpoint)
              : null,
          },
    series,
  }
}
export const QUALITY_HOURS = 24
export const OPS_QUALITY_HOURS = [6, 24, 72] as const
export type OpsQualityHours = (typeof OPS_QUALITY_HOURS)[number]
export type PerformanceAction = 'gc' | 'disk_cache' | 'reset_stats'
export const opsMonitorApi = {
  execute: (action: PerformanceAction) =>
    action === 'disk_cache'
      ? api.delete('/api/performance/disk_cache')
      : api.post(`/api/performance/${action}`),
  stats: async (signal?: AbortSignal, window: OpsWindow = '1h') =>
    parsePerformanceStats(
      await api.get('/api/performance/stats', { window }, { signal })
    ),
  routing: async (signal?: AbortSignal) =>
    parseRoutingSnapshot(
      await api.get('/api/performance/routing', undefined, { signal })
    ),
  quality: async (
    signal?: AbortSignal,
    hours: OpsQualityHours = QUALITY_HOURS
  ) =>
    parsePerformanceQuality(
      await api.get(
        '/api/performance/quality',
        { hours: String(hours) },
        { signal }
      )
    ),
}

export const OPS_WINDOWS = ['1h', '6h', '24h'] as const
export type OpsWindow = (typeof OPS_WINDOWS)[number]
export const OPS_WINDOW_SECONDS: Record<OpsWindow, number> = {
  '1h': 3600,
  '6h': 21600,
  '24h': 86400,
}
export type OpsStatus = ObservationStatus
export interface OpsCheck {
  key: CheckID
  reason?: ObservationReason
  status: OpsStatus
  unavailable_reason?: 'not_provided'
  value: number | null
  threshold: number | null
}
export interface OpsSeriesPoint {
  ts: number
  cpu: number | null
  memory: number | null
  disk: number | null
  users: number | null
  channels: number | null
  inbound_mbps?: number | null
  outbound_mbps?: number | null
  goroutines?: number | null
  heap_alloc_bytes?: number | null
}
export type OpsSeriesField = Exclude<keyof OpsSeriesPoint, 'ts'>
export interface OpsOverview {
  sampled_at: number
  instance_started_at: number | null
  health_score: null
  observation: Observability | null
  status: OpsStatus
  checks: OpsCheck[]
  gateway: RoutingSnapshot | null
  resources: {
    monitor_enabled: boolean | null
    cpu: { percent: number | null; threshold: number | null }
    memory: {
      percent: number | null
      threshold: number | null
      used_bytes: number | null
      total_bytes: number | null
    }
    disk: {
      percent: number | null
      threshold: number | null
      used_bytes: number | null
      total_bytes: number | null
      path: string | null
    }
    network: { up_mbps: number | null; down_mbps: number | null }
    process: {
      goroutines: number | null
      heap_alloc_bytes: number | null
      runtime_sys_bytes: number | null
      gc_count: number | null
      last_gc_pause_ms: number | null
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
/** Go stores monitor thresholds as signed ints; any value <= 0 disables the
 * check (common/system_monitor.go exceedsThreshold), so it is not invalid. */
function monitorThreshold(record: Record<string, unknown>, key: string) {
  const value = record[key]
  return value === undefined || value === null
    ? null
    : requiredStrictInteger(value, '/api/performance/stats')
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
  const observation = stats?.observability ?? null
  const resources = observation?.resources
  const checks: OpsCheck[] = (observation?.checks ?? []).map((check) => {
    const resource =
      check.id === 'cpu' || check.id === 'memory' || check.id === 'disk'
    const probe =
      check.id === 'database'
        ? observation?.storage.database
        : check.id === 'log_database'
          ? observation?.storage.log_database
          : check.id === 'redis'
            ? observation?.storage.redis
            : null
    return {
      key: check.id,
      status: check.status,
      reason: check.reason,
      value: resource
        ? (resources?.[check.id as 'cpu' | 'memory' | 'disk'].percent ?? null)
        : (probe?.latency_ms ?? null),
      threshold: resource
        ? (resources?.[check.id as 'cpu' | 'memory' | 'disk'].threshold ?? null)
        : null,
    }
  })
  const threshold = (key: 'cpu' | 'memory' | 'disk') =>
    resources?.[key].threshold ?? null
  const path = config.disk_cache_path
  return {
    sampled_at: sampledAt,
    instance_started_at: observation?.instance_started_at ?? null,
    health_score: null,
    observation,
    status: observation?.status ?? 'unknown',
    checks,
    gateway: routing,
    cache: stats,
    resources:
      stats && observation && resources
        ? {
            monitor_enabled: performanceBoolean(config, 'monitor_enabled'),
            cpu: {
              percent: resources.cpu.percent,
              threshold: threshold('cpu'),
            },
            memory: {
              percent: resources.memory.percent,
              threshold: threshold('memory'),
              used_bytes: resources.memory.used_bytes,
              total_bytes: resources.memory.total_bytes,
            },
            disk: {
              percent: resources.disk.percent,
              threshold: threshold('disk'),
              used_bytes: resources.disk.used_bytes,
              total_bytes: resources.disk.total_bytes,
              path: typeof path === 'string' ? path : null,
            },
            network: {
              up_mbps: observation.traffic.outbound_mbps,
              down_mbps: observation.traffic.inbound_mbps,
            },
            process: {
              goroutines: observation.process?.goroutines ?? null,
              heap_alloc_bytes: observation.process?.heap_alloc_bytes ?? null,
              runtime_sys_bytes: observation.process?.heap_sys_bytes ?? null,
              gc_count: observation.process?.gc_count ?? null,
              last_gc_pause_ms: observation.process?.last_gc_pause_ms ?? null,
            },
          }
        : null,
    series: observation
      ? {
          window: observation.series.window,
          bucket_seconds: observation.series.bucket_seconds,
          points: observation.series.points.map((point) => {
            const { user_in_flight, channel_in_flight, ...rest } = point
            return {
              ...rest,
              users: user_in_flight ?? null,
              channels: channel_in_flight ?? null,
            }
          }),
        }
      : {
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
