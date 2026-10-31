import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'

const endpoint = '/api/performance/stats'
export type ObservationStatus =
  'ok' | 'warning' | 'critical' | 'unknown' | 'disabled' | 'stale'
export type ObservationReason =
  | ''
  | 'not_sampled'
  | 'disabled'
  | 'stale'
  | 'unavailable'
  | 'timeout'
  | 'canceled'
  | 'threshold_exceeded'
export type ResourceScope = 'container' | 'host' | 'filesystem' | 'unknown'
/** Where traffic is counted, decided once at server start: the host's uplink
 * interface counters, or the bytes on the app's main listener connections. */
export type TrafficScope = 'host_interface' | 'application_listener'
export const TRAFFIC_SCOPES: readonly TrafficScope[] = [
  'host_interface',
  'application_listener',
]
/** Why a configured host interface has no reading; empty when it has one. */
export type TrafficReason =
  | ''
  | 'host_config_invalid'
  | 'host_counters_unreadable'
  | 'host_counters_invalid'
  | 'host_interface_missing'
const TRAFFIC_REASONS: readonly TrafficReason[] = [
  '',
  'host_config_invalid',
  'host_counters_unreadable',
  'host_counters_invalid',
  'host_interface_missing',
]
export type CheckID =
  'cpu' | 'memory' | 'disk' | 'database' | 'log_database' | 'redis'
export interface ResourceObservation {
  available: boolean
  percent: number | null
  scope: ResourceScope
  sampled_at: number | null
  state: ObservationStatus
  used_bytes: number | null
  total_bytes: number | null
  threshold: number
}
export interface ProbeObservation {
  status: ObservationStatus
  latency_ms: number | null
  reason: ObservationReason
}
export interface ObservationPoint {
  ts: number
  cpu: number | null
  memory: number | null
  disk: number | null
  inbound_mbps: number | null
  outbound_mbps: number | null
  goroutines: number | null
  heap_alloc_bytes: number | null
}
export interface Observability {
  scope: 'instance'
  instance_started_at: number
  sampled_at: number | null
  stale: boolean
  status: ObservationStatus
  coverage: { measured: number; total: number }
  checks: {
    id: CheckID
    status: ObservationStatus
    reason: ObservationReason
  }[]
  resources: {
    cpu: ResourceObservation
    memory: ResourceObservation
    disk: ResourceObservation
  }
  storage: {
    database: ProbeObservation
    log_database: ProbeObservation | null
    log_shared: boolean
    redis: ProbeObservation
  }
  process: {
    goroutines: number
    heap_alloc_bytes: number
    heap_sys_bytes: number
    gc_count: number
    last_gc_pause_ms: number | null
  } | null
  /** Rates are seen from the server: inbound is received, outbound is sent. */
  traffic: {
    scope: TrafficScope
    inbound_mbps: number | null
    outbound_mbps: number | null
    /** The configured host interface; empty for the listener scope. */
    interface: string
    reason: TrafficReason
  }
  series: {
    window: '1h' | '6h' | '24h'
    bucket_seconds: number
    points: ObservationPoint[]
  }
}

function record(value: unknown): Record<string, unknown> {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}
function member<T extends string>(value: unknown, choices: readonly T[]): T {
  if (typeof value !== 'string' || !choices.includes(value as T))
    invalidResponse(endpoint)
  return value as T
}
function number(value: unknown, integer = false): number {
  const parsed = integer
    ? requiredStrictInteger(value, endpoint)
    : requiredStrictNumber(value, endpoint)
  if (parsed < 0) invalidResponse(endpoint)
  return parsed
}
function nullable(value: unknown, integer = false): number | null {
  return value === null ? null : number(value, integer)
}
const statuses = [
  'ok',
  'warning',
  'critical',
  'unknown',
  'disabled',
  'stale',
] as const
const reasons = [
  '',
  'not_sampled',
  'disabled',
  'stale',
  'unavailable',
  'timeout',
  'canceled',
  'threshold_exceeded',
] as const
function resource(value: unknown): ResourceObservation {
  const raw = record(value)
  const result: ResourceObservation = {
    available: requiredBoolean(raw.available, endpoint),
    percent: nullable(raw.percent),
    scope: member(raw.scope, ['container', 'host', 'filesystem', 'unknown']),
    sampled_at: nullable(raw.sampled_at, true),
    state: member(raw.state, statuses),
    used_bytes: nullable(raw.used_bytes),
    total_bytes: nullable(raw.total_bytes),
    threshold: requiredStrictInteger(raw.threshold, endpoint),
  }
  if (
    !result.available &&
    (result.percent !== null ||
      result.used_bytes !== null ||
      result.total_bytes !== null)
  )
    invalidResponse(endpoint)
  return result
}
/** A failed host read carries no rate, and only the host scope names an
 * interface; any other combination is a server fault, not a reading. */
function trafficObservation(value: unknown): Observability['traffic'] {
  const raw = record(value)
  const result: Observability['traffic'] = {
    scope: member(raw.scope, TRAFFIC_SCOPES),
    inbound_mbps: nullable(raw.inbound_mbps),
    outbound_mbps: nullable(raw.outbound_mbps),
    interface: requiredString(raw.interface, endpoint),
    reason: member(raw.reason, TRAFFIC_REASONS),
  }
  const hasRate = result.inbound_mbps !== null || result.outbound_mbps !== null
  if (result.reason !== '' && hasRate) invalidResponse(endpoint)
  if (result.scope === 'application_listener' && result.interface !== '')
    invalidResponse(endpoint)
  return result
}
function probe(value: unknown): ProbeObservation {
  const raw = record(value)
  return {
    status: member(raw.status, statuses),
    latency_ms: nullable(raw.latency_ms),
    reason: member(raw.reason, reasons),
  }
}
export function parseObservability(value: unknown): Observability {
  const raw = record(value)
  const coverage = record(raw.coverage)
  const resources = record(raw.resources)
  const storage = record(raw.storage)
  const series = record(raw.series)
  const process = raw.process === null ? null : record(raw.process)
  if (!Array.isArray(raw.checks) || !Array.isArray(series.points))
    invalidResponse(endpoint)
  const measured = number(coverage.measured, true)
  const total = number(coverage.total, true)
  if (measured > total) invalidResponse(endpoint)
  const bucketSeconds = number(series.bucket_seconds, true)
  if (bucketSeconds === 0) invalidResponse(endpoint)
  return {
    scope: member(raw.scope, ['instance']),
    instance_started_at: number(raw.instance_started_at, true),
    sampled_at: nullable(raw.sampled_at, true),
    stale: requiredBoolean(raw.stale, endpoint),
    status: member(raw.status, statuses),
    coverage: { measured, total },
    checks: raw.checks.map((value) => {
      const check = record(value)
      return {
        id: member(check.id, [
          'cpu',
          'memory',
          'disk',
          'database',
          'log_database',
          'redis',
        ]),
        status: member(check.status, statuses),
        reason: member(check.reason, reasons),
      }
    }),
    resources: {
      cpu: resource(resources.cpu),
      memory: resource(resources.memory),
      disk: resource(resources.disk),
    },
    storage: {
      database: probe(storage.database),
      log_database:
        storage.log_database === null ? null : probe(storage.log_database),
      log_shared: requiredBoolean(storage.log_shared, endpoint),
      redis: probe(storage.redis),
    },
    process:
      process === null
        ? null
        : {
            goroutines: number(process.goroutines, true),
            heap_alloc_bytes: number(process.heap_alloc_bytes),
            heap_sys_bytes: number(process.heap_sys_bytes),
            gc_count: number(process.gc_count, true),
            last_gc_pause_ms: nullable(process.last_gc_pause_ms),
          },
    traffic: trafficObservation(raw.traffic),
    series: {
      window: member(series.window, ['1h', '6h', '24h']),
      bucket_seconds: bucketSeconds,
      points: series.points.map((value) => {
        const row = record(value)
        return {
          ts: number(row.ts, true),
          cpu: nullable(row.cpu),
          memory: nullable(row.memory),
          disk: nullable(row.disk),
          inbound_mbps: nullable(row.inbound_mbps),
          outbound_mbps: nullable(row.outbound_mbps),
          goroutines: nullable(row.goroutines),
          heap_alloc_bytes: nullable(row.heap_alloc_bytes),
        }
      }),
    },
  }
}
