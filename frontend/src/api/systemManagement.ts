import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'

export type SystemTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'
export interface SystemTaskQuery {
  scope?: 'active' | 'history'
  type?: string
  status?: 'succeeded' | 'failed' | ''
  offset?: number
  limit?: number
}
export interface SystemTaskPage {
  items: SystemTask[]
  total: number
}
export const SYSTEM_TASK_HEALTH = [
  'ok',
  'failing',
  'stale',
  'idle',
  'running',
  'inactive',
] as const
export type SystemTaskHealth = (typeof SYSTEM_TASK_HEALTH)[number]
/** One registered handler; the summary endpoint is the source of task types. */
export interface SystemTaskTypeSummary {
  type: string
  scheduled: boolean
  enabled: boolean
  interval_seconds: number | null
  last_success_at: number | null
  last_failure_at: number | null
  last_error: string
  active: boolean
  status: SystemTaskHealth
}
export interface SystemTask {
  task_id: string
  type: string
  status: SystemTaskStatus
  created_at: number
  updated_at: number
  error: string
  locked_by: string
  progress: number | null
  processed: number | null
  total: number | null
  deleted_count: number | null
  pricing_result?: PricingSyncResult
}

export interface PricingSyncResult {
  checked: number
  changed: number
  published: number
  deactivated: number
  unconfigured_count: number
  unconfigured: string[]
  errors: string[]
  unconfigured_details: Array<{
    model: string
    reasons: Array<{ source: string; reason: string }>
  }>
}

function parsePricingSyncResult(
  value: Record<string, unknown>,
  endpoint: string
): PricingSyncResult {
  const counters = {} as Pick<
    PricingSyncResult,
    'checked' | 'changed' | 'published' | 'deactivated' | 'unconfigured_count'
  >
  for (const key of [
    'checked',
    'changed',
    'published',
    'deactivated',
    'unconfigured_count',
  ] as const) {
    const count = requiredStrictInteger(value[key], endpoint)
    if (count < 0) invalidResponse(endpoint)
    counters[key] = count
  }
  if (
    !Array.isArray(value.unconfigured) ||
    value.unconfigured.length > 100 ||
    !Array.isArray(value.errors)
  )
    invalidResponse(endpoint)
  const details = value.unconfigured_details ?? []
  if (!Array.isArray(details) || details.length > 100) invalidResponse(endpoint)
  return {
    ...counters,
    unconfigured: value.unconfigured.map((name) =>
      requiredString(name, endpoint, false)
    ),
    errors: value.errors.map((message) => requiredString(message, endpoint)),
    unconfigured_details: details.map((raw) => {
      const detail = record(raw, endpoint)
      if (!Array.isArray(detail.reasons)) invalidResponse(endpoint)
      return {
        model: requiredString(detail.model, endpoint, false),
        reasons: detail.reasons.map((rawReason) => {
          const reason = record(rawReason, endpoint)
          return {
            source: requiredString(reason.source, endpoint, false),
            reason: requiredString(reason.reason, endpoint, false),
          }
        }),
      }
    }),
  }
}

function record(value: unknown, endpoint: string): Record<string, unknown> {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}

function optionalText(value: unknown, endpoint: string): string {
  return value == null ? '' : requiredString(value, endpoint)
}

function optionalNumber(value: unknown, endpoint: string): number | null {
  if (value == null) return null
  const number = requiredStrictNumber(value, endpoint)
  if (number < 0) invalidResponse(endpoint)
  return number
}

function optionalInteger(value: unknown, endpoint: string): number | null {
  const number = optionalNumber(value, endpoint)
  if (number !== null && !Number.isSafeInteger(number))
    invalidResponse(endpoint)
  return number
}

export function parseSystemTaskSummary(
  value: unknown,
  endpoint = '/api/system-task/summary'
): SystemTaskTypeSummary[] {
  const data = record(value, endpoint)
  if (!Array.isArray(data.types)) invalidResponse(endpoint)
  return data.types.map((raw) => {
    const row = record(raw, endpoint)
    if (!SYSTEM_TASK_HEALTH.includes(row.status as SystemTaskHealth))
      invalidResponse(endpoint)
    return {
      type: requiredString(row.type, endpoint, false),
      scheduled: requiredBoolean(row.scheduled, endpoint),
      enabled: requiredBoolean(row.enabled, endpoint),
      interval_seconds: optionalInteger(row.interval_seconds, endpoint),
      last_success_at: optionalInteger(row.last_success_at, endpoint),
      last_failure_at: optionalInteger(row.last_failure_at, endpoint),
      last_error: optionalText(row.last_error, endpoint),
      active: requiredBoolean(row.active, endpoint),
      status: row.status as SystemTaskHealth,
    }
  })
}

export function parseSystemTask(
  value: unknown,
  endpoint = '/api/system-task/list'
): SystemTask {
  const row = record(value, endpoint)
  const state = record(row.state ?? {}, endpoint)
  const result = record(row.result ?? {}, endpoint)
  if (
    !['pending', 'running', 'succeeded', 'failed'].includes(String(row.status))
  )
    invalidResponse(endpoint)
  return {
    task_id: requiredString(row.task_id, endpoint, false),
    type: requiredString(row.type, endpoint, false),
    status: row.status as SystemTaskStatus,
    created_at: requiredStrictInteger(row.created_at, endpoint),
    updated_at: requiredStrictInteger(row.updated_at, endpoint),
    error: optionalText(row.error, endpoint),
    locked_by: optionalText(row.locked_by, endpoint),
    progress: optionalNumber(state.progress, endpoint),
    processed: optionalInteger(state.processed, endpoint),
    total: optionalInteger(state.total, endpoint),
    deleted_count: optionalInteger(result.deleted_count, endpoint),
    ...(row.type === 'billing_pricing_sync' && Object.keys(result).length
      ? { pricing_result: parsePricingSyncResult(result, endpoint) }
      : {}),
  }
}

export function isActiveSystemTask(task: SystemTask): boolean {
  return task.status === 'pending' || task.status === 'running'
}

export const systemManagementApi = {
  async summary(signal?: AbortSignal): Promise<SystemTaskTypeSummary[]> {
    const endpoint = '/api/system-task/summary'
    return parseSystemTaskSummary(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
  },
  async tasks(
    query: SystemTaskQuery,
    signal?: AbortSignal
  ): Promise<SystemTaskPage> {
    const endpoint = '/api/system-task/list'
    const response = await api.getEnvelope(endpoint, { ...query }, { signal })
    if (!Array.isArray(response.data)) invalidResponse(endpoint)
    const total = requiredStrictInteger(response.total, endpoint)
    if (total < 0) invalidResponse(endpoint)
    return {
      items: response.data.map((item) => parseSystemTask(item, endpoint)),
      total,
    }
  },
  async deleteHistory(
    filters: Pick<SystemTaskQuery, 'type' | 'status'>
  ): Promise<number> {
    const endpoint = '/api/system-task/history'
    const data = record(
      await api.delete(endpoint, { scope: 'history', ...filters }),
      endpoint
    )
    const count = requiredStrictInteger(data.deleted_count, endpoint)
    if (count < 0) invalidResponse(endpoint)
    return count
  },
  async task(taskId: string, signal?: AbortSignal): Promise<SystemTask> {
    const endpoint = `/api/system-task/${encodeURIComponent(taskId)}`
    return parseSystemTask(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
  },
  async cleanup(target: number): Promise<SystemTask> {
    if (
      !Number.isSafeInteger(target) ||
      target <= 0 ||
      target * 1000 >= Date.now()
    )
      throw new RangeError('Invalid log cleanup timestamp')
    const endpoint = `/api/system-task/log-cleanup?target_timestamp=${target}`
    return parseSystemTask(await api.post(endpoint), endpoint)
  },
}
