import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'

export interface SystemInstance {
  node_name: string
  display_name: string
  status: 'online' | 'stale'
  stale_after_seconds: number | null
  last_seen_at: number
  started_at: number
  name_source: string
  manually_configured: boolean | null
  should_configure_manually: boolean | null
  version: string
  hostname: string
  goos: string
  goarch: string
  master: boolean | null
  cpu: number | null
  memory: number | null
  storage: number | null
  storage_total: number | null
  storage_used: number | null
  storage_free: number | null
}

export type SystemTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'
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

function optionalBoolean(value: unknown, endpoint: string): boolean | null {
  return value == null ? null : requiredBoolean(value, endpoint)
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

export function parseSystemInstance(
  value: unknown,
  endpoint = '/api/system-info/instances'
): SystemInstance {
  const row = record(value, endpoint)
  if (row.status !== 'online' && row.status !== 'stale')
    invalidResponse(endpoint)
  const info = record(row.info ?? {}, endpoint)
  const node = record(info.node ?? {}, endpoint)
  const runtime = record(info.runtime ?? {}, endpoint)
  const host = record(info.host ?? {}, endpoint)
  const role = record(info.role ?? {}, endpoint)
  const resources = record(info.resources ?? {}, endpoint)
  const cpu = record(resources.cpu ?? {}, endpoint)
  const memory = record(resources.memory ?? {}, endpoint)
  const storage = record(resources.storage ?? {}, endpoint)
  const nodeName = requiredString(row.node_name, endpoint, false)
  return {
    node_name: nodeName,
    display_name: optionalText(node.name, endpoint) || nodeName,
    status: row.status,
    stale_after_seconds: optionalInteger(row.stale_after_seconds, endpoint),
    last_seen_at: requiredStrictInteger(row.last_seen_at, endpoint),
    started_at: requiredStrictInteger(row.started_at, endpoint),
    name_source: optionalText(node.source, endpoint),
    manually_configured: optionalBoolean(node.manually_configured, endpoint),
    should_configure_manually: optionalBoolean(
      node.should_configure_manually,
      endpoint
    ),
    version: optionalText(runtime.version, endpoint),
    hostname: optionalText(host.hostname, endpoint),
    goos: optionalText(runtime.goos, endpoint),
    goarch: optionalText(runtime.goarch, endpoint),
    master: optionalBoolean(role.is_master, endpoint),
    cpu: optionalNumber(cpu.usage_percent, endpoint),
    memory: optionalNumber(memory.usage_percent, endpoint),
    storage: optionalNumber(storage.used_percent, endpoint),
    storage_total: optionalInteger(storage.total_bytes, endpoint),
    storage_used: optionalInteger(storage.used_bytes, endpoint),
    storage_free: optionalInteger(storage.free_bytes, endpoint),
  }
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
  async instances(signal?: AbortSignal): Promise<SystemInstance[]> {
    const endpoint = '/api/system-info/instances'
    const data = await api.get(endpoint, undefined, { signal })
    if (!Array.isArray(data)) invalidResponse(endpoint)
    return data.map((item) => parseSystemInstance(item, endpoint))
  },
  async tasks(limit = 20, signal?: AbortSignal): Promise<SystemTask[]> {
    const endpoint = '/api/system-task/list'
    const data = await api.get(endpoint, { limit }, { signal })
    if (!Array.isArray(data)) invalidResponse(endpoint)
    return data.map((item) => parseSystemTask(item, endpoint))
  },
  async task(taskId: string, signal?: AbortSignal): Promise<SystemTask> {
    const endpoint = `/api/system-task/${encodeURIComponent(taskId)}`
    return parseSystemTask(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
  },
  async deleteStale(name?: string): Promise<number> {
    const endpoint = name
      ? `/api/system-info/instances/${encodeURIComponent(name)}`
      : '/api/system-info/stale-instances'
    const data = record(await api.delete(endpoint), endpoint)
    const count = requiredStrictInteger(data.deleted_count, endpoint)
    if (count < 0) invalidResponse(endpoint)
    return count
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
