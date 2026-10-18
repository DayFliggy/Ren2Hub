import { api } from './client'
import {
  invalidResponse,
  isRecord,
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
  }
}

export function isActiveSystemTask(task: SystemTask): boolean {
  return task.status === 'pending' || task.status === 'running'
}

export const systemManagementApi = {
  async current(
    type: string,
    signal?: AbortSignal
  ): Promise<SystemTask | null> {
    const endpoint = '/api/system-task/current'
    const data = await api.get(endpoint, { type }, { signal })
    return data === null ? null : parseSystemTask(data, endpoint)
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
