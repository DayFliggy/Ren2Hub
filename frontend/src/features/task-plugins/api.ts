// Mirrors upstream NewAPI web/src/features/task-plugins/api.ts on Ren2Hub's
// shared client. Metadata preview is local; upload validation belongs to the server.
import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import { previewPluginSource } from './lib/plugin-source-preview'

import type {
  MarketplaceSource,
  TaskPluginDetail,
  TaskPluginDryRunRequest,
  TaskPluginListItem,
  TaskPluginOption,
  TaskPluginRecord,
  TaskPluginUsage,
  TaskPluginChangeNotice,
  TaskPluginStatusResult,
} from './types'

const base = '/api/plugin/task'
const keyPath = (key: string) => `${base}/${encodeURIComponent(key)}`

export class TaskPluginUsageError extends Error {
  constructor(
    message: string,
    public readonly usage: TaskPluginUsage
  ) {
    super(message)
    this.name = 'TaskPluginUsageError'
  }
}

function parseUsage(value: unknown): TaskPluginUsage | null {
  if (!value || typeof value !== 'object') return null
  const usage = value as TaskPluginUsage
  const validChannels =
    Array.isArray(usage.channels) &&
    usage.channels.every(
      (channel) =>
        Boolean(channel) &&
        Number.isSafeInteger(channel.id) &&
        channel.id > 0 &&
        typeof channel.name === 'string' &&
        Number.isSafeInteger(channel.type)
    )
  if (
    !validChannels ||
    !Number.isSafeInteger(usage.in_flight_count) ||
    usage.in_flight_count < 0
  )
    return null
  return usage
}

/** Re-throws an in-use business rejection as a typed usage error. */
async function withUsage<T>(request: Promise<T>): Promise<T> {
  try {
    return await request
  } catch (error) {
    const usage =
      error instanceof ApiError && error.business
        ? parseUsage(error.data)
        : null
    if (usage) throw new TaskPluginUsageError((error as Error).message, usage)
    throw error
  }
}

export function listTaskPlugins(signal?: AbortSignal) {
  return api.get<TaskPluginListItem[]>(base, undefined, { signal })
}

export function getTaskPlugin(
  key: string,
  version?: string,
  signal?: AbortSignal
) {
  return api.get<TaskPluginDetail>(
    keyPath(key),
    version ? { version } : undefined,
    { signal }
  )
}

export function getTaskPluginVersions(key: string, signal?: AbortSignal) {
  return api.get<TaskPluginRecord[]>(`${keyPath(key)}/versions`, undefined, {
    signal,
  })
}

/** `icon` is the sidecar icon.svg / icon.png as a data URI, stored apart from the source. */
export function uploadTaskPlugin(request: {
  source: string
  sourceSha256?: string
  remark: string
  icon?: string
}) {
  return api.post<TaskPluginDetail>(base, {
    source: request.source,
    sourceSha256: request.sourceSha256 || undefined,
    remark: request.remark,
    icon: request.icon || undefined,
  })
}

/**
 * Installs a marketplace version. `sourceSha256` lets the server re-hash the
 * bytes it received; `force` is never sent, so a routing conflict rejects.
 */
export function installMarketplacePlugin(request: {
  source: string
  sourceSha256?: string
  remark: string
  icon?: string
  marketplaceIndexUrl?: string
}) {
  return api.post<TaskPluginDetail>(base, {
    source: request.source,
    sourceSha256: request.sourceSha256 || undefined,
    enabled: true,
    marketplaceIndexUrl: request.marketplaceIndexUrl,
    remark: request.remark,
    icon: request.icon || undefined,
  })
}

/** Literal metadata preview; this does not call a separate server endpoint. */
export function previewTaskPlugin(source: string, signal?: AbortSignal) {
  return previewPluginSource(source, signal)
}

export async function listMarketplaceSources(signal?: AbortSignal) {
  return (
    (await api.get<MarketplaceSource[] | null>(
      `${base}/marketplace/sources`,
      undefined,
      { signal }
    )) ?? []
  )
}

export async function updateMarketplaceSources(sources: MarketplaceSource[]) {
  return (
    (await api.put<MarketplaceSource[] | null>(
      `${base}/marketplace/sources`,
      sources
    )) ?? []
  )
}

export function activateTaskPlugin(key: string, version: string) {
  return api.post<TaskPluginChangeNotice>(`${keyPath(key)}/activate`, {
    version,
  })
}

export function setTaskPluginStatus(
  key: string,
  enabled: boolean,
  options?: { cascade?: boolean; force?: boolean }
) {
  const params = new URLSearchParams()
  if (!enabled && options?.cascade) params.set('cascade', 'true')
  if (!enabled && options?.force) params.set('force', 'true')
  const query = params.size ? `?${params}` : ''
  const request = api.post<TaskPluginStatusResult>(
    `${keyPath(key)}/status${query}`,
    { enabled }
  )
  // Only a disable can be blocked by usage; an enable error stays as-is.
  return enabled ? request : withUsage(request)
}

/** force=true deletes a version that channels or in-flight tasks still use. */
export function deleteTaskPluginVersion(
  key: string,
  version: string,
  force = false
) {
  const query = force ? '?force=true' : ''
  return withUsage(
    api.delete<TaskPluginChangeNotice | null>(
      `${keyPath(key)}/versions/${encodeURIComponent(version)}${query}`
    )
  )
}

export function dryRunTaskPlugin(
  key: string,
  request: TaskPluginDryRunRequest
) {
  return api.post<unknown>(`${keyPath(key)}/dryrun`, request)
}

export function getTaskPluginOptions(signal?: AbortSignal) {
  return api.get<TaskPluginOption[]>('/api/task_plugin_options', undefined, {
    signal,
  })
}

export async function getTaskPluginEnabledOption(signal?: AbortSignal) {
  const options = await api.get<Array<{ key: string; value: string }>>(
    '/api/option/',
    undefined,
    { signal }
  )
  return (
    options.find((option) => option.key === 'TaskPluginEnabled')?.value ===
    'true'
  )
}

export function setTaskPluginEnabledOption(enabled: boolean) {
  return api.put<null>('/api/option/', {
    key: 'TaskPluginEnabled',
    value: String(enabled),
  })
}

function parseRequestPluginInstances(
  result: unknown
): import('./types').RequestPluginInstance[] {
  if (
    !result ||
    typeof result !== 'object' ||
    !('instances' in result) ||
    (result.instances !== null && !Array.isArray(result.instances))
  )
    throw new Error('Invalid request plugin instances response')
  return (result.instances ?? []).map((instance: unknown) => {
    if (!instance || typeof instance !== 'object')
      throw new Error('Invalid request plugin instance')
    const row = instance as Record<string, unknown>
    if (
      typeof row.id !== 'string' ||
      typeof row.name !== 'string' ||
      typeof row.plugin !== 'string' ||
      typeof row.enabled !== 'boolean' ||
      typeof row.global !== 'boolean' ||
      (row.models != null &&
        (!Array.isArray(row.models) ||
          row.models.some((model) => typeof model !== 'string'))) ||
      (row.config != null &&
        (typeof row.config !== 'object' || Array.isArray(row.config)))
    )
      throw new Error('Invalid request plugin instance')
    // Non-root bind reads redact config; Go may also encode absent maps/lists as null.
    return {
      id: row.id,
      name: row.name,
      plugin: row.plugin,
      enabled: row.enabled,
      global: row.global,
      models: (row.models ?? []) as string[],
      config: (row.config ?? {}) as Record<string, unknown>,
    }
  })
}
export async function listRequestPluginInstances(signal?: AbortSignal) {
  return parseRequestPluginInstances(
    await api.get('/api/request_plugin/instances', undefined, { signal })
  )
}
export async function saveRequestPluginInstances(
  instances: import('./types').RequestPluginInstance[],
  signal?: AbortSignal
) {
  return parseRequestPluginInstances(
    await api.put('/api/request_plugin/instances', { instances }, { signal })
  )
}

export function dryRunRequestPlugins(
  request: import('./types').RequestPluginDryRunRequest,
  signal?: AbortSignal
) {
  return api.post<import('./types').RequestPluginDryRunResult>(
    '/api/request_plugin/dry_run',
    request,
    { signal }
  )
}
