import { api } from './client'
import type { PriceVersion } from './modelPricing'

export interface PluginMeta {
  key: string
  name: string
  version: string
  apiVersion: number
  icon?: string
  description?: Record<string, string>
  models: string[]
  channelTypes?: number[]
  upstreams?: string[]
  allowedHosts?: string[]
  protocols?: Array<{ name: string; supports?: string[] }>
  usageSchema?: Record<string, unknown>
  usageProfiles?: unknown[]
}
export interface TaskPlugin {
  meta: PluginMeta
  source: string
  enabled: boolean
  active: boolean
  source_hash: string
  has_icon?: boolean
  runtime_status: string
  runtime_error?: string
  channel_count: number
  in_flight_count: number
}
export interface PluginVersion {
  key: string
  version: string
  source_hash: string
  source: string
  active: boolean
  enabled: boolean
  factory: boolean
}
export interface PluginDetail {
  meta: PluginMeta
  source: string
  layer: string
}
export interface MarketSource {
  name: string
  index_url: string
}
export interface MarketVersion {
  version: string
  path: string
  sha256: string
  minApiVersion?: number
}
export interface MarketPlugin {
  key: string
  name: string
  description: string | Record<string, string>
  icon?: string
  iconURL?: string
  latest: string
  versions: MarketVersion[]
  indexURL: string
  sourceName: string
}
export interface PluginRuntime {
  current_generation: number
  database_revision: string
  last_rebuild: { status: string; error?: string }
  plugin_errors: Record<string, string>
}
const base = '/api/plugin/task'
const keyPath = (key: string) => `${base}/${encodeURIComponent(key)}`
export const taskPluginsApi = {
  list: (signal?: AbortSignal) => api.get<TaskPlugin[]>(base, {}, { signal }),
  runtime: (signal?: AbortSignal) =>
    api.get<PluginRuntime>(`${base}/runtime/status`, {}, { signal }),
  sources: (signal?: AbortSignal) =>
    api.get<MarketSource[]>(`${base}/marketplace/sources`, {}, { signal }),
  saveSources: (sources: MarketSource[]) =>
    api.put(`${base}/marketplace/sources`, sources),
  versions: (key: string, signal?: AbortSignal) =>
    api.get<PluginVersion[]>(`${keyPath(key)}/versions`, {}, { signal }),
  detail: (key: string, version?: string) =>
    api.get<PluginDetail>(keyPath(key), { version }),
  preview: (source: string) =>
    api.post<{
      meta: Partial<PluginMeta>
      source_hash: string
      unresolved: string[]
    }>(`${base}/preview`, {
      source,
    }),
  upload: (source: string, sourceSha256: string, enabled: boolean) =>
    api.post<PluginDetail>(base, { source, sourceSha256, enabled }),
  activate: (key: string, version: string) =>
    api.post(`${keyPath(key)}/activate`, { version }),
  enabled: (key: string, enabled: boolean) =>
    api.post(`${keyPath(key)}/status`, { enabled }),
  remove: (key: string, version: string) =>
    api.delete(`${keyPath(key)}/versions/${encodeURIComponent(version)}`),
  dryRun: (key: string, hook: string, member: string, args: unknown[]) =>
    api.post(`${keyPath(key)}/dryrun`, { hook, member, args }),
  priceDraft: (key: string, model: string, expression: string) =>
    api.post<{ price_version: PriceVersion }>(`${keyPath(key)}/price-draft`, {
      model,
      expression,
    }),
  options: (signal?: AbortSignal) =>
    api.get<PluginMeta[]>('/api/task_plugin_options', {}, { signal }),
}

export function marketSourceURL(indexURL: string, path: string): string {
  const baseURL = new URL(indexURL)
  const resolved = new URL(path, baseURL)
  if (
    baseURL.protocol !== 'https:' ||
    resolved.origin !== baseURL.origin ||
    resolved.username ||
    resolved.password
  )
    throw new Error('Invalid marketplace source URL')
  return resolved.href
}

export function parseMarketIndex(
  value: unknown,
  source: MarketSource
): MarketPlugin[] {
  if (!value || typeof value !== 'object')
    throw new Error('Invalid marketplace index')
  const index = value as Record<string, unknown>
  if (index.indexVersion !== 1 || !Array.isArray(index.plugins))
    throw new Error('Unsupported marketplace index')
  return index.plugins.flatMap((entry: unknown) => {
    if (!entry || typeof entry !== 'object') return []
    const row = entry as Record<string, unknown>
    if (typeof row.key !== 'string' || !Array.isArray(row.versions)) return []
    const versions = row.versions.flatMap((item: unknown) => {
      if (!item || typeof item !== 'object') return []
      const v = item as Record<string, unknown>
      if (
        typeof v.version !== 'string' ||
        typeof v.path !== 'string' ||
        typeof v.sha256 !== 'string' ||
        !/^[a-f0-9]{64}$/i.test(v.sha256) ||
        (v.kind !== undefined && v.kind !== 'task')
      )
        return []
      marketSourceURL(source.index_url, v.path)
      return [
        {
          version: v.version,
          path: v.path,
          sha256: v.sha256.toLowerCase(),
          minApiVersion:
            typeof v.minApiVersion === 'number' ? v.minApiVersion : undefined,
        },
      ]
    })
    if (!versions.length) return []
    const description =
      typeof row.description === 'string'
        ? row.description
        : row.description &&
            typeof row.description === 'object' &&
            !Array.isArray(row.description)
          ? Object.fromEntries(
              Object.entries(row.description).filter(
                (entry): entry is [string, string] =>
                  typeof entry[1] === 'string'
              )
            )
          : ''
    const iconFile = row.iconFile as { path?: unknown } | undefined
    const iconURL =
      typeof iconFile?.path === 'string'
        ? marketSourceURL(source.index_url, iconFile.path)
        : undefined
    return [
      {
        key: row.key,
        name: typeof row.name === 'string' ? row.name : row.key,
        description,
        icon: typeof row.icon === 'string' ? row.icon : undefined,
        iconURL,
        latest:
          typeof row.latest === 'string' ? row.latest : versions[0]!.version,
        versions,
        indexURL: source.index_url,
        sourceName: source.name,
      },
    ]
  })
}

export class MarketplaceDownloadError extends Error {
  constructor(public readonly status: number) {
    super(`Marketplace HTTP ${status}`)
  }
}

export async function readMarketText(
  url: string,
  limit: number,
  signal?: AbortSignal
): Promise<string> {
  const response = await fetch(marketSourceURL(url, url), {
    signal,
    credentials: 'omit',
    redirect: 'error',
    referrerPolicy: 'no-referrer',
  })
  if (!response.ok || !response.body)
    throw new MarketplaceDownloadError(response.status)
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let size = 0
  try {
    for (;;) {
      const next = await reader.read()
      if (next.done) break
      size += next.value.length
      if (size > limit)
        throw new Error('Marketplace payload exceeds size limit')
      chunks.push(next.value)
    }
  } finally {
    await reader.cancel()
  }
  const bytes = new Uint8Array(size)
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.length
  }
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
}

export async function pluginSourceHash(source: string): Promise<string> {
  const hash = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(source)
  )
  return Array.from(new Uint8Array(hash), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('')
}
