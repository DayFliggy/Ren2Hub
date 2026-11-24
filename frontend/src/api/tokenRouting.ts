import { api } from './client'
import { isDecimal } from '@/utils/decimal'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredString,
  parseStringArray,
} from './contracts'

export interface TokenRouteEntry {
  channel_id: number
  enabled: boolean
  position: number
}
export type TokenRouteMode = 'auto' | 'ordered'
export type TokenRouteScope = 'platform' | 'selected' | 'empty'

/**
 * Persisted token routing profile plus backend-derived effective scope/mode.
 * selected_only=false uses every platform channel; selected_only=true with all
 * entries disabled falls back to platform auto routing while preserving entries;
 * selected_only=true with 0 entries is the orphaned empty scope (503).
 */
export interface TokenRoute {
  token_id: number
  mode: TokenRouteMode
  selected_only: boolean
  version: number
  max_channel_ratio: string | null
  entries: TokenRouteEntry[]
  route_scope?: TokenRouteScope
  effective_mode?: TokenRouteMode
  enabled_route_channel_count?: number
}

export interface TokenRouteChannel {
  id: number
  name: string
  type: number
  status: number
  models: string[]
  channel_ratio: string
  max_concurrency: number
  eligible: boolean
  filter_reason?: string
}

export interface TokenRouteCandidateQuery {
  model_limits?: string[]
  max_channel_ratio?: string
}

const endpoint = (id: number) => `/api/token/${id}/routing`
const candidatesEndpoint = '/api/token/routing/channels'

function parseMaxRatio(value: unknown): string | null {
  if (value === null) return null
  if (!isDecimal(value) || Number(value) <= 0)
    invalidResponse('/api/token/:id/routing')
  return value
}

function parseRouteScope(
  value: unknown,
  path: string
): TokenRouteScope | undefined {
  if (value === undefined) return undefined
  if (value === 'platform' || value === 'selected' || value === 'empty')
    return value
  invalidResponse(path)
}

function parseRouteMode(
  value: unknown,
  path: string
): TokenRouteMode | undefined {
  if (value === undefined) return undefined
  if (value === 'auto' || value === 'ordered') return value
  invalidResponse(path)
}

export function parseTokenRoute(value: unknown): TokenRoute {
  if (
    !isRecord(value) ||
    (value.mode !== 'auto' && value.mode !== 'ordered') ||
    !Array.isArray(value.entries)
  )
    invalidResponse('/api/token/:id/routing')
  const entries = value.entries.map((entry) => {
    if (!isRecord(entry)) invalidResponse('/api/token/:id/routing')
    return {
      channel_id: requiredStrictInteger(entry.channel_id, endpoint(0)),
      enabled: requiredBoolean(entry.enabled, endpoint(0)),
      position: requiredStrictInteger(entry.position, endpoint(0)),
    }
  })
  const selectedOnly = requiredBoolean(
    value.selected_only,
    '/api/token/:id/routing'
  )
  const enabledCount =
    value.enabled_route_channel_count === undefined
      ? entries.filter((entry) => entry.enabled).length
      : requiredStrictInteger(
          value.enabled_route_channel_count,
          '/api/token/:id/routing'
        )
  const scope =
    parseRouteScope(value.route_scope, '/api/token/:id/routing') ??
    (!selectedOnly
      ? 'platform'
      : entries.length === 0
        ? 'empty'
        : enabledCount === 0
          ? 'platform'
          : 'selected')
  const effectiveMode =
    parseRouteMode(value.effective_mode, '/api/token/:id/routing') ??
    (scope === 'selected' && value.mode === 'ordered' ? 'ordered' : 'auto')
  return {
    token_id: requiredStrictInteger(value.token_id, '/api/token/:id/routing'),
    mode: value.mode,
    selected_only: selectedOnly,
    version: requiredStrictInteger(value.version, '/api/token/:id/routing'),
    max_channel_ratio: parseMaxRatio(value.max_channel_ratio),
    entries,
    route_scope: scope,
    effective_mode: effectiveMode,
    enabled_route_channel_count: enabledCount,
  }
}

function parseRouteChannels(
  value: unknown,
  source: string
): TokenRouteChannel[] {
  if (!Array.isArray(value)) invalidResponse(source)
  return value.map((item) => {
    if (!isRecord(item)) invalidResponse(source)
    return {
      id: requiredStrictInteger(item.id, source),
      name: requiredString(item.name, source),
      type: requiredStrictInteger(item.type, source),
      status: requiredStrictInteger(item.status, source),
      models: parseStringArray(item.models, source),
      channel_ratio: parseMaxRatio(item.channel_ratio)!,
      max_concurrency: requiredStrictInteger(item.max_concurrency, source),
      eligible: requiredBoolean(item.eligible, source),
      filter_reason:
        item.filter_reason === undefined
          ? undefined
          : requiredString(item.filter_reason, source),
    }
  })
}

export function parseCreatedTokenId(value: unknown): number | null {
  if (!isRecord(value) || value.id === undefined) return null
  const id = requiredStrictInteger(value.id, '/api/token/')
  return id > 0 ? id : null
}

export const tokenRoutingApi = {
  async get(id: number, signal?: AbortSignal) {
    return parseTokenRoute(await api.get(endpoint(id), undefined, { signal }))
  },
  async save(
    id: number,
    value: Pick<
      TokenRoute,
      'mode' | 'selected_only' | 'version' | 'entries' | 'max_channel_ratio'
    >
  ) {
    // Platform scope stores no entries; selected scope preserves both enabled
    // and paused entries so users keep their saved list order.
    return parseTokenRoute(
      await api.put(endpoint(id), {
        mode: value.mode,
        selected_only: value.selected_only,
        version: value.version,
        max_channel_ratio: value.max_channel_ratio,
        entries: value.selected_only
          ? value.entries.map(({ channel_id, enabled }) => ({
              channel_id,
              enabled,
            }))
          : [],
      })
    )
  },
  async candidates(
    query: TokenRouteCandidateQuery = {},
    signal?: AbortSignal
  ): Promise<TokenRouteChannel[]> {
    const params: Record<string, string> = {}
    const limits = (query.model_limits ?? [])
      .map((item) => item.trim())
      .filter(Boolean)
    if (limits.length > 0) params.model_limits = limits.join(',')
    const ratio = query.max_channel_ratio?.trim() ?? ''
    if (ratio) params.max_channel_ratio = ratio
    const value = await api.get<unknown>(
      candidatesEndpoint,
      Object.keys(params).length > 0 ? params : undefined,
      { signal }
    )
    return parseRouteChannels(value, candidatesEndpoint)
  },
  async channels(
    id: number,
    signal?: AbortSignal
  ): Promise<TokenRouteChannel[]> {
    const value = await api.get<unknown>(
      `${endpoint(id)}/channels`,
      undefined,
      { signal }
    )
    return parseRouteChannels(value, `${endpoint(id)}/channels`)
  },
}
