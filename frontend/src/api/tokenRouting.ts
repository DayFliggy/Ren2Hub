import { api } from './client'
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
  entries: TokenRouteEntry[]
}
export interface TokenRouteChannel {
  id: number
  name: string
  type: number
  status: number
  models: string[]
}
const endpoint = (id: number) => `/api/token/${id}/routing`
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
  groups: string[]
  entries: Array<{
    channel_id: number
    name?: string
    eligible: boolean
    reason: string
    group?: string
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
      groups: parseStringArray(value.groups, url),
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
          group:
            item.group === undefined
              ? undefined
              : requiredString(item.group, url),
        }
      }),
    }
  },
  async get(id: number, signal?: AbortSignal) {
    return parseTokenRoute(await api.get(endpoint(id), undefined, { signal }))
  },
  async save(
    id: number,
    value: Pick<TokenRoute, 'mode' | 'version' | 'entries'>
  ) {
    return parseTokenRoute(
      await api.put(endpoint(id), {
        mode: value.mode,
        version: value.version,
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
      }
    })
  },
}
