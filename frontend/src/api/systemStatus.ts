import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
} from './contracts'
import { TRAFFIC_SCOPES, type TrafficScope } from './observability'

const endpoint = '/api/user/system-status'

export type SystemHealth = 'ok' | 'warning' | 'critical' | 'stale' | 'unknown'
export type SystemResourceState =
  'ok' | 'warning' | 'critical' | 'unchecked' | 'unknown' | 'disabled' | 'stale'
export type SystemResourceScope =
  'container' | 'host' | 'filesystem' | 'unknown'
export type SuccessRateState = 'ok' | 'no_requests' | 'disabled' | 'error'
/** Worst database/Redis probe verdict, without probe names or reasons. */
export type SystemStorageState = 'ok' | 'critical' | 'stale' | 'unknown'

export interface SystemResource {
  available: boolean
  /** A measured resource is "ok", or "warning" past the health check's
   * threshold; the server owns that rule. "critical" is a contract value the
   * resource checks do not produce yet. "unchecked" is a reading whose
   * threshold check is off; "disabled" means resource monitoring is off. */
  state: SystemResourceState
  scope: SystemResourceScope
  percent: number | null
  used_bytes: number | null
  total_bytes: number | null
}
/** Rates are seen from the server: inbound is received, outbound is sent. */
export interface SystemTrafficPoint {
  ts: number
  inbound_mbps: number | null
  outbound_mbps: number | null
}
/** Host-wide kernel run-queue average; cores is null when unreadable. */
export interface SystemLoad {
  scope: 'host'
  one: number
  five: number
  fifteen: number
  cores: number | null
}
export interface SystemStatus {
  status: SystemHealth
  stale: boolean
  sampled_at: number | null
  cpu: SystemResource
  memory: SystemResource
  disk: SystemResource
  load: SystemLoad | null
  traffic: {
    scope: TrafficScope
    inbound_mbps: number | null
    outbound_mbps: number | null
    bucket_seconds: number
    points: SystemTrafficPoint[]
  }
  /** rate is set only for state "ok"; 24h window ending now. */
  success_rate: {
    state: SuccessRateState
    rate: number | null
    window_start: number | null
    window_end: number | null
    checked_at: number
  }
  storage_status: SystemStorageState
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
function resource(value: unknown): SystemResource {
  const raw = record(value)
  const result: SystemResource = {
    available: requiredBoolean(raw.available, endpoint),
    state: member(raw.state, [
      'ok',
      'warning',
      'critical',
      'unchecked',
      'unknown',
      'disabled',
      'stale',
    ]),
    scope: member(raw.scope, ['container', 'host', 'filesystem', 'unknown']),
    percent: nullable(raw.percent),
    used_bytes: nullable(raw.used_bytes),
    total_bytes: nullable(raw.total_bytes),
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
function load(value: unknown): SystemLoad | null {
  if (value === null) return null
  const raw = record(value)
  const cores = nullable(raw.cores, true)
  if (cores === 0) invalidResponse(endpoint)
  return {
    scope: member(raw.scope, ['host']),
    one: number(raw.one),
    five: number(raw.five),
    fifteen: number(raw.fifteen),
    cores,
  }
}
function successRate(value: unknown): SystemStatus['success_rate'] {
  const raw = record(value)
  const state = member(raw.state, ['ok', 'no_requests', 'disabled', 'error'])
  const rate = nullable(raw.rate)
  if ((state === 'ok') !== (rate !== null) || (rate !== null && rate > 100))
    invalidResponse(endpoint)
  return {
    state,
    rate,
    window_start: nullable(raw.window_start, true),
    window_end: nullable(raw.window_end, true),
    checked_at: number(raw.checked_at, true),
  }
}

export function parseSystemStatus(value: unknown): SystemStatus {
  const raw = record(value)
  const traffic = record(raw.traffic)
  if (!Array.isArray(traffic.points)) invalidResponse(endpoint)
  const bucketSeconds = number(traffic.bucket_seconds, true)
  if (bucketSeconds === 0) invalidResponse(endpoint)
  return {
    status: member(raw.status, [
      'ok',
      'warning',
      'critical',
      'stale',
      'unknown',
    ]),
    stale: requiredBoolean(raw.stale, endpoint),
    sampled_at: nullable(raw.sampled_at, true),
    cpu: resource(raw.cpu),
    memory: resource(raw.memory),
    disk: resource(raw.disk),
    load: load(raw.load),
    traffic: {
      scope: member(traffic.scope, TRAFFIC_SCOPES),
      inbound_mbps: nullable(traffic.inbound_mbps),
      outbound_mbps: nullable(traffic.outbound_mbps),
      bucket_seconds: bucketSeconds,
      points: traffic.points.map((value) => {
        const row = record(value)
        return {
          ts: number(row.ts, true),
          inbound_mbps: nullable(row.inbound_mbps),
          outbound_mbps: nullable(row.outbound_mbps),
        }
      }),
    },
    success_rate: successRate(raw.success_rate),
    storage_status: member(raw.storage_status, [
      'ok',
      'critical',
      'stale',
      'unknown',
    ]),
  }
}

export const systemStatusApi = {
  get: async (signal?: AbortSignal) =>
    parseSystemStatus(await api.get(endpoint, undefined, { signal })),
}
