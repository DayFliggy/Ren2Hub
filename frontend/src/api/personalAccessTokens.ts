import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { proofOptions } from './security'

const endpoint = '/api/user/access_tokens'

export interface PersonalAccessToken {
  id: number
  name: string
  scopes: string[]
  created_at: number
  last_used_at: number
  expires_at: number
  token_ref: string
  token_hint: string
  last_used_ip: string
}

export interface PATScopeCatalog {
  scopes: string[]
  limit: number
  default_lifetime_days: number
}

export function parsePersonalAccessToken(value: unknown): PersonalAccessToken {
  if (!isRecord(value)) invalidResponse(endpoint)
  const result = {
    id: requiredStrictInteger(value.id, endpoint),
    name: requiredString(value.name, endpoint, false),
    scopes: parseStringArray(value.scopes, endpoint),
    created_at: requiredStrictInteger(value.created_at, endpoint),
    last_used_at: requiredStrictInteger(value.last_used_at, endpoint),
    expires_at: requiredStrictInteger(value.expires_at, endpoint),
    token_ref: requiredString(value.token_ref, endpoint, false),
    token_hint: requiredString(value.token_hint, endpoint),
    last_used_ip: requiredString(value.last_used_ip, endpoint),
  }
  if (
    result.id <= 0 ||
    result.scopes.length === 0 ||
    [result.created_at, result.last_used_at, result.expires_at].some(
      (v) => v < 0
    )
  )
    invalidResponse(endpoint)
  return result
}

export const personalAccessTokensApi = {
  async list(signal?: AbortSignal) {
    const result = await api.get<unknown>(endpoint, undefined, { signal })
    if (!isRecord(result) || !Array.isArray(result.items))
      invalidResponse(endpoint)
    return result.items.map(parsePersonalAccessToken)
  },
  async scopes(signal?: AbortSignal): Promise<PATScopeCatalog> {
    const result = await api.get<unknown>(`${endpoint}/catalog`, undefined, {
      signal,
    })
    if (!isRecord(result) || !Array.isArray(result.groups))
      invalidResponse(endpoint)
    const scopes: string[] = []
    for (const group of result.groups) {
      if (!isRecord(group) || !Array.isArray(group.resources))
        invalidResponse(endpoint)
      for (const resource of group.resources) {
        if (!isRecord(resource) || !Array.isArray(resource.actions))
          invalidResponse(endpoint)
        const name = requiredString(resource.resource, endpoint, false)
        for (const action of resource.actions) {
          if (!isRecord(action)) invalidResponse(endpoint)
          scopes.push(
            `${name}:${requiredString(action.action, endpoint, false)}`
          )
        }
      }
    }
    const catalog = {
      scopes,
      limit: requiredStrictInteger(result.max_tokens, endpoint),
      default_lifetime_days: requiredStrictInteger(
        result.default_expiry_days,
        endpoint
      ),
    }
    if (catalog.limit < 1 || catalog.default_lifetime_days < 1)
      invalidResponse(endpoint)
    return catalog
  },
  async create(
    input: { name: string; scopes: string[]; expires_at: number },
    proof: string,
    signal?: AbortSignal
  ) {
    const result = await api.post<unknown>(endpoint, input, {
      ...proofOptions(proof),
      signal,
    })
    if (!isRecord(result)) invalidResponse(endpoint)
    const token = requiredString(result.token, endpoint, false)
    if (!/^nap_[A-Za-z0-9]{43}$/.test(token)) invalidResponse(endpoint)
    return {
      token,
      item: parsePersonalAccessToken(result.item),
    }
  },
  async update(
    id: number,
    input: { name: string; scopes?: string[] },
    proof?: string,
    signal?: AbortSignal
  ) {
    return parsePersonalAccessToken(
      await api.patch<unknown>(`${endpoint}/${id}`, input, {
        ...(proof ? proofOptions(proof) : {}),
        signal,
      })
    )
  },
  revoke: (id: number, proof: string, signal?: AbortSignal) =>
    api.delete(`${endpoint}/${id}`, undefined, {
      ...proofOptions(proof),
      signal,
    }),
}
