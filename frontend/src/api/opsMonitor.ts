import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'

/**
 * Contract for the admin ops monitor (`/api/ops/*`). `null` always means
 * "not measured"; parsers keep it so the UI can render "--" instead of zero.
 */
export const OPS_STATUSES = ['ok', 'warning', 'critical', 'unknown'] as const
export type OpsStatus = (typeof OPS_STATUSES)[number]

export const OPS_WINDOWS = ['1h', '6h', '24h'] as const
export type OpsWindow = (typeof OPS_WINDOWS)[number]
export const OPS_WINDOW_SECONDS: Record<OpsWindow, number> = {
  '1h': 3600,
  '6h': 21_600,
  '24h': 86_400,
}

export const OPS_CHECK_KEYS = [
  'cpu',
  'memory',
  'disk',
  'success_rate',
  'admission',
  'draining',
  'master',
  'database',
  'log_database',
  'redis',
] as const
export type OpsCheckKey = (typeof OPS_CHECK_KEYS)[number]

export const OPS_PROBE_ERRORS = ['timeout', 'unavailable'] as const
export type OpsProbeError = (typeof OPS_PROBE_ERRORS)[number]

export const OPS_BREAKER_STATES = ['open', 'half_open'] as const
export type OpsBreakerState = (typeof OPS_BREAKER_STATES)[number]

export interface OpsCheck {
  key: OpsCheckKey
  status: OpsStatus
  value: number | null
  /** 0 means the limit is disabled. */
  threshold: number | null
  error: OpsProbeError | null
}

export interface OpsRuntime {
  version: string
  go_version: string
  goos: string
  goarch: string
  node_name: string
  started_at: number
  uptime_seconds: number
  master: boolean
  draining: boolean
}

export interface OpsUsage {
  percent: number | null
  used_bytes: number | null
  total_bytes: number | null
  threshold: number
}

export interface OpsResources {
  monitor_enabled: boolean
  cpu: { percent: number | null; threshold: number }
  memory: OpsUsage
  disk: OpsUsage & { path: string }
  network: { up_mbps: number | null; down_mbps: number | null }
  process: {
    goroutines: number
    heap_alloc_bytes: number
    heap_sys_bytes: number
    gc_count: number
    last_gc_pause_ms: number
  }
}

export interface OpsQueue {
  active: number
  entered_total: number
  completed_total: number
  failed_total: number
  wait_ms_total: number
}

export interface OpsGateway {
  admission: {
    in_flight: number
    queued: number
    total_queue_size: number
    pools: number
    granted_total: number
    released_total: number
    draining: boolean
    queue_full_total: number
    total_queue_full_total: number
    wait_timeout_total: number
    canceled_total: number
    /** Cumulative counts: bucket i holds waits <= wait_bucket_bounds_ms[i]. */
    wait_duration_ms_buckets: number[]
  }
  /** Ascending upper bounds; the final `null` is +Inf. */
  wait_bucket_bounds_ms: Array<number | null>
  retry_budget: {
    enabled: boolean
    available_hundredths: number
    reserved: number
    first_sends_total: number
    retry_sends_total: number
    rejected_total: number
  }
  user_queue: OpsQueue
  channel_queue: OpsQueue
  success_rate: number | null
  granted_per_minute: number | null
  rejected_per_minute: number | null
}

export interface OpsDatabaseProbe {
  kind: string
  status: OpsStatus
  latency_ms: number | null
  error: OpsProbeError | null
  open: number
  in_use: number
  idle: number
  max_open: number
  wait_count: number
}

export interface OpsRedisProbe {
  enabled: boolean
  status: OpsStatus
  latency_ms: number | null
  error: OpsProbeError | null
  total_conns: number
  idle_conns: number
  stale_conns: number
}

export interface OpsStorage {
  database: OpsDatabaseProbe
  /** `null` when logs share the main database. */
  log_database: OpsDatabaseProbe | null
  redis: OpsRedisProbe
}

export const OPS_SERIES_FIELDS = [
  'cpu',
  'memory',
  'disk',
  'net_up_mbps',
  'net_down_mbps',
  'goroutines',
  'heap_alloc_bytes',
  'in_flight',
  'queued',
  'requests_per_minute',
  'success_rate',
  'granted_per_minute',
  'rejected_per_minute',
] as const
export type OpsSeriesField = (typeof OPS_SERIES_FIELDS)[number]
export type OpsSeriesPoint = { ts: number } & Record<
  OpsSeriesField,
  number | null
>

export interface OpsSeries {
  window: OpsWindow
  bucket_seconds: number
  points: OpsSeriesPoint[]
}

export interface OpsOverview {
  schema_version: 1
  sampled_at: number
  status: OpsStatus
  checks: OpsCheck[]
  runtime: OpsRuntime
  resources: OpsResources
  gateway: OpsGateway
  storage: OpsStorage
  series: OpsSeries
}

export interface OpsTraffic {
  requests: number
  /** `null` when there were no requests. */
  success_rate: number | null
  avg_latency_ms: number
  avg_tps: number
}

export interface OpsRequests {
  enabled: boolean
  hours: number
  bucket_seconds: number
  summary: OpsTraffic
  series: Array<OpsTraffic & { ts: number }>
  top_models: Array<OpsTraffic & { model_name: string }>
}

export interface OpsUnhealthyChannel {
  channel_id: number
  channel_name: string
  model: string
  state: OpsBreakerState
  failure_count: number
  cooldown_until: number | null
  last_latency_ms: number | null
  first_token_latency_ms: number | null
  updated_at: number
}

export interface OpsChannels {
  channels: {
    total: number
    enabled: number
    manually_disabled: number
    auto_disabled: number
  }
  breakers: { closed: number; open: number; half_open: number }
  unhealthy: OpsUnhealthyChannel[]
}

function record(value: unknown, endpoint: string): Record<string, unknown> {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}

function oneOf<T extends string>(
  value: unknown,
  allowed: readonly T[],
  endpoint: string
): T {
  if (!allowed.includes(value as T)) invalidResponse(endpoint)
  return value as T
}

function count(value: unknown, endpoint: string): number {
  const parsed = requiredStrictInteger(value, endpoint)
  if (parsed < 0) invalidResponse(endpoint)
  return parsed
}

function amount(value: unknown, endpoint: string): number {
  const parsed = requiredStrictNumber(value, endpoint)
  if (parsed < 0) invalidResponse(endpoint)
  return parsed
}

/** A present field whose `null` value means "not measured". */
function measured(value: unknown, endpoint: string): number | null {
  if (value === null) return null
  return amount(value, endpoint)
}

function measuredCount(value: unknown, endpoint: string): number | null {
  if (value === null) return null
  return count(value, endpoint)
}

/** Healthy probes report "" and failed probes name the failure. */
function probeError(value: unknown, endpoint: string): OpsProbeError | null {
  if (value === '') return null
  return oneOf(value, OPS_PROBE_ERRORS, endpoint)
}

function parseCheck(value: unknown, endpoint: string): OpsCheck {
  const row = record(value, endpoint)
  return {
    key: oneOf(row.key, OPS_CHECK_KEYS, endpoint),
    status: oneOf(row.status, OPS_STATUSES, endpoint),
    // The backend omits these when they do not apply to the check.
    value: row.value == null ? null : requiredStrictNumber(row.value, endpoint),
    threshold: row.threshold == null ? null : amount(row.threshold, endpoint),
    error:
      row.error == null ? null : oneOf(row.error, OPS_PROBE_ERRORS, endpoint),
  }
}

function parseRuntime(value: unknown, endpoint: string): OpsRuntime {
  const row = record(value, endpoint)
  return {
    version: requiredString(row.version, endpoint),
    go_version: requiredString(row.go_version, endpoint),
    goos: requiredString(row.goos, endpoint),
    goarch: requiredString(row.goarch, endpoint),
    node_name: requiredString(row.node_name, endpoint),
    started_at: count(row.started_at, endpoint),
    uptime_seconds: count(row.uptime_seconds, endpoint),
    master: requiredBoolean(row.master, endpoint),
    draining: requiredBoolean(row.draining, endpoint),
  }
}

function parseUsage(value: unknown, endpoint: string): OpsUsage {
  const row = record(value, endpoint)
  return {
    percent: measured(row.percent, endpoint),
    used_bytes: measuredCount(row.used_bytes, endpoint),
    total_bytes: measuredCount(row.total_bytes, endpoint),
    threshold: amount(row.threshold, endpoint),
  }
}

function parseResources(value: unknown, endpoint: string): OpsResources {
  const row = record(value, endpoint)
  const cpu = record(row.cpu, endpoint)
  const disk = record(row.disk, endpoint)
  const network = record(row.network, endpoint)
  const process = record(row.process, endpoint)
  return {
    monitor_enabled: requiredBoolean(row.monitor_enabled, endpoint),
    cpu: {
      percent: measured(cpu.percent, endpoint),
      threshold: amount(cpu.threshold, endpoint),
    },
    memory: parseUsage(row.memory, endpoint),
    disk: {
      ...parseUsage(disk, endpoint),
      // The backend omits an empty path.
      path: disk.path === undefined ? '' : requiredString(disk.path, endpoint),
    },
    network: {
      up_mbps: measured(network.up_mbps, endpoint),
      down_mbps: measured(network.down_mbps, endpoint),
    },
    process: {
      goroutines: count(process.goroutines, endpoint),
      heap_alloc_bytes: count(process.heap_alloc_bytes, endpoint),
      heap_sys_bytes: count(process.heap_sys_bytes, endpoint),
      gc_count: count(process.gc_count, endpoint),
      last_gc_pause_ms: amount(process.last_gc_pause_ms, endpoint),
    },
  }
}
function parseQueue(value: unknown, endpoint: string): OpsQueue {
  const row = record(value, endpoint)
  return {
    active: count(row.active, endpoint),
    entered_total: count(row.entered_total, endpoint),
    completed_total: count(row.completed_total, endpoint),
    failed_total: count(row.failed_total, endpoint),
    wait_ms_total: count(row.wait_ms_total, endpoint),
  }
}

function parseGateway(value: unknown, endpoint: string): OpsGateway {
  const row = record(value, endpoint)
  const admission = record(row.admission, endpoint)
  const retry = record(row.retry_budget, endpoint)
  const bounds = row.wait_bucket_bounds_ms
  const buckets = admission.wait_duration_ms_buckets
  if (
    !Array.isArray(bounds) ||
    !Array.isArray(buckets) ||
    bounds.length < 2 ||
    buckets.length !== bounds.length ||
    bounds.at(-1) !== null
  )
    invalidResponse(endpoint)
  const finiteBounds = bounds
    .slice(0, -1)
    .map((bound) => amount(bound, endpoint))
  const cumulative = buckets.map((bucket) => count(bucket, endpoint))
  if (
    finiteBounds.some((bound, i) => i > 0 && bound <= finiteBounds[i - 1]!) ||
    cumulative.some((bucket, i) => i > 0 && bucket < cumulative[i - 1]!)
  )
    invalidResponse(endpoint)
  return {
    admission: {
      in_flight: count(admission.in_flight, endpoint),
      queued: count(admission.queued, endpoint),
      total_queue_size: count(admission.total_queue_size, endpoint),
      pools: count(admission.pools, endpoint),
      granted_total: count(admission.granted_total, endpoint),
      released_total: count(admission.released_total, endpoint),
      draining: requiredBoolean(admission.draining, endpoint),
      queue_full_total: count(admission.queue_full_total, endpoint),
      total_queue_full_total: count(admission.total_queue_full_total, endpoint),
      wait_timeout_total: count(admission.wait_timeout_total, endpoint),
      canceled_total: count(admission.canceled_total, endpoint),
      wait_duration_ms_buckets: cumulative,
    },
    wait_bucket_bounds_ms: [...finiteBounds, null],
    retry_budget: {
      enabled: requiredBoolean(retry.enabled, endpoint),
      available_hundredths: count(retry.available_hundredths, endpoint),
      reserved: count(retry.reserved, endpoint),
      first_sends_total: count(retry.first_sends_total, endpoint),
      retry_sends_total: count(retry.retry_sends_total, endpoint),
      rejected_total: count(retry.rejected_total, endpoint),
    },
    user_queue: parseQueue(row.user_queue, endpoint),
    channel_queue: parseQueue(row.channel_queue, endpoint),
    success_rate: measured(row.success_rate, endpoint),
    granted_per_minute: measured(row.granted_per_minute, endpoint),
    rejected_per_minute: measured(row.rejected_per_minute, endpoint),
  }
}

function parseDatabaseProbe(
  value: unknown,
  endpoint: string
): OpsDatabaseProbe {
  const row = record(value, endpoint)
  return {
    kind: requiredString(row.kind, endpoint, false),
    status: oneOf(row.status, OPS_STATUSES, endpoint),
    latency_ms: measured(row.latency_ms, endpoint),
    error: probeError(row.error, endpoint),
    open: count(row.open, endpoint),
    in_use: count(row.in_use, endpoint),
    idle: count(row.idle, endpoint),
    max_open: count(row.max_open, endpoint),
    wait_count: count(row.wait_count, endpoint),
  }
}

function parseRedisProbe(value: unknown, endpoint: string): OpsRedisProbe {
  const row = record(value, endpoint)
  return {
    enabled: requiredBoolean(row.enabled, endpoint),
    status: oneOf(row.status, OPS_STATUSES, endpoint),
    latency_ms: measured(row.latency_ms, endpoint),
    error: probeError(row.error, endpoint),
    total_conns: count(row.total_conns, endpoint),
    idle_conns: count(row.idle_conns, endpoint),
    stale_conns: count(row.stale_conns, endpoint),
  }
}

function parseSeries(value: unknown, endpoint: string): OpsSeries {
  const row = record(value, endpoint)
  if (!Array.isArray(row.points)) invalidResponse(endpoint)
  const bucketSeconds = count(row.bucket_seconds, endpoint)
  if (bucketSeconds === 0) invalidResponse(endpoint)
  const points = row.points.map((raw) => {
    const point = record(raw, endpoint)
    const parsed = { ts: count(point.ts, endpoint) } as OpsSeriesPoint
    for (const field of OPS_SERIES_FIELDS)
      parsed[field] = measured(point[field], endpoint)
    return parsed
  })
  if (points.some((point, i) => i > 0 && point.ts <= points[i - 1]!.ts))
    invalidResponse(endpoint)
  return {
    window: oneOf(row.window, OPS_WINDOWS, endpoint),
    bucket_seconds: bucketSeconds,
    points,
  }
}

export function parseOpsOverview(
  value: unknown,
  endpoint = '/api/ops/overview'
): OpsOverview {
  const row = record(value, endpoint)
  if (row.schema_version !== 1 || !Array.isArray(row.checks))
    invalidResponse(endpoint)
  const storage = record(row.storage, endpoint)
  return {
    schema_version: 1,
    sampled_at: count(row.sampled_at, endpoint),
    status: oneOf(row.status, OPS_STATUSES, endpoint),
    checks: row.checks.map((check) => parseCheck(check, endpoint)),
    runtime: parseRuntime(row.runtime, endpoint),
    resources: parseResources(row.resources, endpoint),
    gateway: parseGateway(row.gateway, endpoint),
    storage: {
      database: parseDatabaseProbe(storage.database, endpoint),
      log_database:
        storage.log_database === null
          ? null
          : parseDatabaseProbe(storage.log_database, endpoint),
      redis: parseRedisProbe(storage.redis, endpoint),
    },
    series: parseSeries(row.series, endpoint),
  }
}

function parseTraffic(
  row: Record<string, unknown>,
  endpoint: string
): OpsTraffic {
  return {
    requests: count(row.requests, endpoint),
    success_rate: measured(row.success_rate, endpoint),
    avg_latency_ms: amount(row.avg_latency_ms, endpoint),
    avg_tps: amount(row.avg_tps, endpoint),
  }
}

export function parseOpsRequests(
  value: unknown,
  endpoint = '/api/ops/requests'
): OpsRequests {
  const row = record(value, endpoint)
  if (
    !Array.isArray(row.series) ||
    !Array.isArray(row.top_models) ||
    row.top_models.length > 6
  )
    invalidResponse(endpoint)
  const hours = count(row.hours, endpoint)
  const bucketSeconds = count(row.bucket_seconds, endpoint)
  if (hours < 1 || hours > 168 || bucketSeconds === 0) invalidResponse(endpoint)
  const series = row.series.map((raw) => {
    const bucket = record(raw, endpoint)
    return { ts: count(bucket.ts, endpoint), ...parseTraffic(bucket, endpoint) }
  })
  if (series.some((bucket, i) => i > 0 && bucket.ts <= series[i - 1]!.ts))
    invalidResponse(endpoint)
  return {
    enabled: requiredBoolean(row.enabled, endpoint),
    hours,
    bucket_seconds: bucketSeconds,
    summary: parseTraffic(record(row.summary, endpoint), endpoint),
    series,
    top_models: row.top_models.map((raw) => {
      const model = record(raw, endpoint)
      return {
        model_name: requiredString(model.model_name, endpoint),
        ...parseTraffic(model, endpoint),
      }
    }),
  }
}

export function parseOpsChannels(
  value: unknown,
  endpoint = '/api/ops/channels'
): OpsChannels {
  const row = record(value, endpoint)
  const channels = record(row.channels, endpoint)
  const breakers = record(row.breakers, endpoint)
  if (!Array.isArray(row.unhealthy) || row.unhealthy.length > 50)
    invalidResponse(endpoint)
  return {
    channels: {
      total: count(channels.total, endpoint),
      enabled: count(channels.enabled, endpoint),
      manually_disabled: count(channels.manually_disabled, endpoint),
      auto_disabled: count(channels.auto_disabled, endpoint),
    },
    breakers: {
      closed: count(breakers.closed, endpoint),
      open: count(breakers.open, endpoint),
      half_open: count(breakers.half_open, endpoint),
    },
    unhealthy: row.unhealthy.map((raw) => {
      const item = record(raw, endpoint)
      return {
        channel_id: count(item.channel_id, endpoint),
        channel_name: requiredString(item.channel_name, endpoint),
        model: requiredString(item.model, endpoint),
        state: oneOf(item.state, OPS_BREAKER_STATES, endpoint),
        failure_count: count(item.failure_count, endpoint),
        cooldown_until: measuredCount(item.cooldown_until, endpoint),
        last_latency_ms: measured(item.last_latency_ms, endpoint),
        first_token_latency_ms: measured(item.first_token_latency_ms, endpoint),
        updated_at: count(item.updated_at, endpoint),
      }
    }),
  }
}

export const opsMonitorApi = {
  async overview(window: OpsWindow, signal?: AbortSignal) {
    const endpoint = '/api/ops/overview'
    const overview = parseOpsOverview(
      await api.get(endpoint, { window }, { signal }),
      endpoint
    )
    // A mismatched series would be labelled with the wrong window.
    if (overview.series.window !== window) invalidResponse(endpoint)
    return overview
  },
  async requests(hours: number, signal?: AbortSignal) {
    const endpoint = '/api/ops/requests'
    const requests = parseOpsRequests(
      await api.get(endpoint, { hours }, { signal }),
      endpoint
    )
    if (requests.hours !== hours) invalidResponse(endpoint)
    return requests
  },
  async channels(signal?: AbortSignal) {
    const endpoint = '/api/ops/channels'
    return parseOpsChannels(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
  },
}
