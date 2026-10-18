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
export interface TokenRoute {
  token_id: number
  mode: 'auto' | 'manual'
  version: number
  max_channel_ratio: string | null
  entries: TokenRouteEntry[]
}
export interface TokenRouteChannel {
  id: number
  name: string
  type: number
  status: number
  models: string[]
  channel_ratio: string
  max_concurrency: number
  in_flight: number
  eligible: boolean
  filter_reason?: string
}
const endpoint = (id: number) => `/api/token/${id}/routing`
function parseMaxRatio(value: unknown): string | null {
  if (value === null) return null
  if (!isDecimal(value) || Number(value) <= 0)
    invalidResponse('/api/token/:id/routing')
  return value
}
export function parseTokenRoute(value: unknown): TokenRoute {
  if (
    !isRecord(value) ||
    (value.mode !== 'auto' && value.mode !== 'manual') ||
    !Array.isArray(value.entries)
  )
    invalidResponse('/api/token/:id/routing')
  return {
    token_id: requiredStrictInteger(value.token_id, '/api/token/:id/routing'),
    mode: value.mode,
    version: requiredStrictInteger(value.version, '/api/token/:id/routing'),
    max_channel_ratio: parseMaxRatio(value.max_channel_ratio),
    entries: value.entries.map((entry) => {
      if (!isRecord(entry)) invalidResponse('/api/token/:id/routing')
      return {
        channel_id: requiredStrictInteger(entry.channel_id, endpoint(0)),
        enabled: requiredBoolean(entry.enabled, endpoint(0)),
        position: requiredStrictInteger(entry.position, endpoint(0)),
      }
    }),
  }
}
export interface TokenRoutePreview {
  mode: 'auto' | 'manual'
  version: number
  model: string
  path: string
  entries: Array<{
    channel_id: number
    name?: string
    eligible: boolean
    reason: string
    channel_ratio: string
    in_flight: number
  }>
}
export const tokenRoutingApi = {
  async preview(
    id: number,
    model: string,
    path: string,
    signal?: AbortSignal
  ): Promise<TokenRoutePreview> {
    const url = `${endpoint(id)}/preview`
    const value = await api.get<unknown>(url, { model, path }, { signal })
    if (
      !isRecord(value) ||
      (value.mode !== 'auto' && value.mode !== 'manual') ||
      !Array.isArray(value.entries)
    )
      invalidResponse(url)
    return {
      mode: value.mode,
      version: requiredStrictInteger(value.version, url),
      model: requiredString(value.model, url),
      path: requiredString(value.path, url),
      entries: value.entries.map((item) => {
        if (!isRecord(item)) invalidResponse(url)
        return {
          channel_id: requiredStrictInteger(item.channel_id, url),
          name:
            item.name === undefined
              ? undefined
              : requiredString(item.name, url),
          eligible: requiredBoolean(item.eligible, url),
          reason: requiredString(item.reason, url),
          channel_ratio: requiredString(item.channel_ratio, url),
          in_flight: requiredStrictInteger(item.in_flight, url),
        }
      }),
    }
  },
  async get(id: number, signal?: AbortSignal) {
    return parseTokenRoute(await api.get(endpoint(id), undefined, { signal }))
  },
  async save(
    id: number,
    value: Pick<
      TokenRoute,
      'mode' | 'version' | 'entries' | 'max_channel_ratio'
    >
  ) {
    return parseTokenRoute(
      await api.put(endpoint(id), {
        mode: value.mode,
        version: value.version,
        max_channel_ratio: value.max_channel_ratio,
        entries:
          value.mode === 'auto'
            ? []
            : value.entries.map(({ channel_id, enabled }) => ({
                channel_id,
                enabled,
              })),
      })
    )
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
    if (!Array.isArray(value)) invalidResponse(endpoint(id))
    return value.map((item) => {
      if (!isRecord(item)) invalidResponse(endpoint(id))
      return {
        id: requiredStrictInteger(item.id, endpoint(id)),
        name: requiredString(item.name, endpoint(id)),
        type: requiredStrictInteger(item.type, endpoint(id)),
        status: requiredStrictInteger(item.status, endpoint(id)),
        models: parseStringArray(item.models, endpoint(id)),
        channel_ratio: parseMaxRatio(item.channel_ratio)!,
        max_concurrency: requiredStrictInteger(
          item.max_concurrency,
          endpoint(id)
        ),
        in_flight: requiredStrictInteger(item.in_flight, endpoint(id)),
        eligible: requiredBoolean(item.eligible, endpoint(id)),
        filter_reason:
          item.filter_reason === undefined
            ? undefined
            : requiredString(item.filter_reason, endpoint(id)),
      }
    })
  },
}
