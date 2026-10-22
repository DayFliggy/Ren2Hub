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

/** The pre-scope users.access_token credential, listed until retire_at. */
export interface LegacyAccessToken {
  token_hint: string
  token_ref: string
  created_at: number | null
  last_used_at: number | null
  last_used_ip: string
  /** Unix seconds after which the legacy token stops working. */
  retire_at: number
}

export interface PersonalAccessTokenList {
  items: PersonalAccessToken[]
  legacy: LegacyAccessToken | null
}

export type PATScopeGroupName = 'personal' | 'admin' | 'system'

/** One grantable action; label_key and description_key are backend English text. */
export interface PATScopeAction {
  action: string
  scope: string
  label_key: string
  description_key: string
}

export interface PATScopeResource {
  resource: string
  label_key: string
  actions: PATScopeAction[]
}

export interface PATScopeGroup {
  group: PATScopeGroupName
  resources: PATScopeResource[]
}

/** GET /api/user/access_tokens/catalog: the scopes this user may grant now. */
export interface PATScopeCatalog {
  groups: PATScopeGroup[]
  /** Every `resource:action` scope in groups. */
  scopes: string[]
  limit: number
  default_lifetime_days: number
}

function isScopeGroupName(value: unknown): value is PATScopeGroupName {
  return value === 'personal' || value === 'admin' || value === 'system'
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

// model.UserAccessTokenStatus keeps these as *int64: no record is null, not 0.
function nullableTimestamp(value: unknown): number | null {
  if (value === null) return null
  const seconds = requiredStrictInteger(value, endpoint)
  if (seconds < 0) invalidResponse(endpoint)
  return seconds
}

function parseLegacyAccessToken(value: unknown): LegacyAccessToken | null {
  if (value === null) return null
  if (!isRecord(value)) invalidResponse(endpoint)
  const retireAt = requiredStrictInteger(value.retire_at, endpoint)
  if (retireAt <= 0) invalidResponse(endpoint)
  return {
    token_hint: requiredString(value.token_hint, endpoint),
    token_ref: requiredString(value.token_ref, endpoint, false),
    created_at: nullableTimestamp(value.created_at),
    last_used_at: nullableTimestamp(value.last_used_at),
    last_used_ip: requiredString(value.last_used_ip, endpoint),
    retire_at: retireAt,
  }
}

/** The resource a `resource:action` scope names. */
export function scopeResource(scope: string): string {
  return scope.split(':')[0]
}

function parseScopeResource(value: unknown): PATScopeResource {
  if (!isRecord(value) || !Array.isArray(value.actions))
    invalidResponse(endpoint)
  const resource = requiredString(value.resource, endpoint, false)
  return {
    resource,
    label_key: requiredString(value.label_key, endpoint, false),
    actions: value.actions.map((action: unknown) => {
      if (!isRecord(action)) invalidResponse(endpoint)
      const name = requiredString(action.action, endpoint, false)
      return {
        action: name,
        scope: `${resource}:${name}`,
        label_key: requiredString(action.label_key, endpoint, false),
        description_key: requiredString(action.description_key, endpoint),
      }
    }),
  }
}

function parseScopeGroup(value: unknown): PATScopeGroup {
  if (!isRecord(value) || !Array.isArray(value.resources))
    invalidResponse(endpoint)
  if (!isScopeGroupName(value.group)) invalidResponse(endpoint)
  return {
    group: value.group,
    resources: value.resources.map(parseScopeResource),
  }
}

export function parsePATScopeCatalog(value: unknown): PATScopeCatalog {
  if (!isRecord(value) || !Array.isArray(value.groups))
    invalidResponse(endpoint)
  const groups = value.groups.map(parseScopeGroup)
  const catalog = {
    groups,
    scopes: groups.flatMap((group) =>
      group.resources.flatMap((resource) =>
        resource.actions.map((action) => action.scope)
      )
    ),
    limit: requiredStrictInteger(value.max_tokens, endpoint),
    default_lifetime_days: requiredStrictInteger(
      value.default_expiry_days,
      endpoint
    ),
  }
  if (catalog.limit < 1 || catalog.default_lifetime_days < 1)
    invalidResponse(endpoint)
  return catalog
}

export const personalAccessTokensApi = {
  async list(signal?: AbortSignal): Promise<PersonalAccessTokenList> {
    const result = await api.get<unknown>(endpoint, undefined, { signal })
    if (!isRecord(result) || !Array.isArray(result.items))
      invalidResponse(endpoint)
    return {
      items: result.items.map(parsePersonalAccessToken),
      legacy: parseLegacyAccessToken(result.legacy),
    }
  },
  async scopes(signal?: AbortSignal): Promise<PATScopeCatalog> {
    return parsePATScopeCatalog(
      await api.get<unknown>(`${endpoint}/catalog`, undefined, { signal })
    )
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
  revokeLegacy: (proof: string, signal?: AbortSignal) =>
    api.delete(`${endpoint}/legacy`, undefined, {
      ...proofOptions(proof),
      signal,
    }),
}
