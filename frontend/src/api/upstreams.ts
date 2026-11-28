import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import { ApiError } from './types'
import { isDecimal } from '@/utils/decimal'

export type UpstreamPlatform = 'newapi' | 'sub2api'
export const UPSTREAM_PLATFORMS: readonly UpstreamPlatform[] = [
  'sub2api',
  'newapi',
]

export interface UpstreamGroup {
  /** Sub2API group ID; NewAPI groups are keyed by name only. */
  id: number | null
  name: string
  platform: string
  /** Decimal text, NewAPI's "auto" sentinel, or "" when the upstream gave none. */
  ratio: string
}

export interface UpstreamSubscription {
  id: number
  upstream_group_id: number
  status: string
  /** Remote time text, passed through as the upstream sent it. */
  expires_at: string
}

/** NewAPI display settings; absent fields are null (old snapshots, Sub2API). */
export interface UpstreamQuotaDisplay {
  quota_per_unit: number | null
  quota_display_type: string
  usd_exchange_rate: number | null
  custom_currency_symbol: string
  custom_currency_exchange_rate: number | null
}

export interface UpstreamSiteMeta extends UpstreamQuotaDisplay {
  username: string
  /** Sub2API: USD balance. NewAPI: raw quota units. */
  balance: string
  upstream_group: string
}

export interface UpstreamCapabilities extends UpstreamQuotaDisplay {
  platform: UpstreamPlatform
  version: string
  site_name: string
  captcha_enabled: boolean
  turnstile_enabled: boolean
  password_login_available: boolean
  totp_enabled: boolean | null
}

export interface UpstreamAccount {
  id: number
  name: string
  platform: UpstreamPlatform
  base_url: string
  has_proxy: boolean
  status: string
  last_error: string
  last_sync_at: number
  next_sync_at: number
  upstream_groups: UpstreamGroup[]
  subscriptions: UpstreamSubscription[]
  site_meta: UpstreamSiteMeta
}

export interface UpstreamRemoteKey {
  id: number
  name: string
  remote_key_prefix: string
  status: string
  upstream_group: string
  upstream_group_id: number
  /** Sub2API: USD limit (0 = unlimited), used USD, RFC3339 expiry or ''. */
  quota: number | null
  quota_used: number | null
  expires_at: string | null
  /** NewAPI: raw quota units and Unix expiry (-1 = never). */
  remain_quota: number | null
  used_quota: number | null
  unlimited_quota: boolean | null
  expired_time: number | null
}

export interface UpstreamBinding {
  id: number
  account_id: number
  remote_key_id: number
  channel_id: number
  channel_created_time: number
  stale: boolean
  remote_status: string
  ratio_sync_enabled: boolean
  ratio_sync_factor: string
  ratio_sync_error: string
}

export interface UpstreamAuthInput {
  account_id?: number
  platform?: UpstreamPlatform
  name?: string
  base_url?: string
  proxy_url?: string
  username?: string
  password?: string
  pat?: string
  refresh_token?: string
}

export type UpstreamAuthResult =
  | { kind: 'account'; account_id: number }
  | { kind: 'challenge'; challenge: string }

export interface UpstreamKeyInput {
  name?: string
  upstream_group?: string
  upstream_group_id?: number
  quota?: number
  expires_in_days?: number
  expires_at?: string
  expired_time?: number
  remain_quota?: number
  unlimited_quota?: boolean
}

export interface UpstreamChannelInput {
  name: string
  models: string
  base_url?: string
}

export interface UpstreamBindingUpdate {
  ratio_sync_enabled?: boolean
  ratio_sync_factor?: string
}

export interface UpstreamEndpointUpdate {
  base_url?: string
  proxy_url?: string
}

const root = '/api/admin/upstreams'
// Remote calls can chain several upstream requests in one local request.
const REMOTE_TIMEOUT_MS = 120_000
const MAX_BINDING_QUERY_IDS = 500
const NUMBER_TEXT = /^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?$/

/** Keeps the business code and, for 429 answers, the Retry-After seconds. */
export class UpstreamRequestError extends ApiError {
  readonly retryAfter: number | null

  constructor(source: ApiError, retryAfter: number | null) {
    super(source.message, {
      status: source.status,
      code: source.code,
      business: source.business,
      data: source.data,
      envelope: source.envelope,
      cause: source.cause ?? source,
    })
    this.name = 'UpstreamRequestError'
    this.retryAfter = retryAfter
  }
}

function headerValue(headers: unknown, name: string): unknown {
  if (!headers || typeof headers !== 'object') return undefined
  const getter = (headers as { get?: unknown }).get
  if (typeof getter === 'function') return getter.call(headers, name)
  return (headers as Record<string, unknown>)[name]
}

function retryAfterOf(error: ApiError): number | null {
  if (error.status !== 429) return null
  const response = (error.cause as { response?: { headers?: unknown } })
    ?.response
  const value = headerValue(response?.headers, 'retry-after')
  const text = Array.isArray(value) ? value[0] : value
  if (typeof text !== 'string' || !/^\d+$/.test(text.trim())) return null
  return Number(text.trim())
}

async function call<T>(task: () => Promise<T>): Promise<T> {
  try {
    return await task()
  } catch (error) {
    if (error instanceof ApiError && !(error instanceof UpstreamRequestError))
      throw new UpstreamRequestError(error, retryAfterOf(error))
    throw error
  }
}

function positiveId(value: number, endpoint: string): number {
  if (!Number.isSafeInteger(value) || value <= 0)
    throw new RangeError(`Invalid id for ${endpoint}`)
  return value
}

function requireNull(value: unknown, endpoint: string): void {
  if (value !== null) invalidResponse(endpoint)
}

function requiredId(value: unknown, endpoint: string): number {
  const id = requiredStrictInteger(value, endpoint)
  if (id <= 0) invalidResponse(endpoint)
  return id
}

function requiredTime(value: unknown, endpoint: string): number {
  const time = requiredStrictInteger(value, endpoint)
  if (time < 0) invalidResponse(endpoint)
  return time
}

function numberText(value: unknown, endpoint: string, allowAuto = false) {
  const text = requiredString(value, endpoint)
  if (text === '' || (allowAuto && text === 'auto')) return text
  if (!NUMBER_TEXT.test(text)) invalidResponse(endpoint)
  return text
}

function optionalText(value: unknown, endpoint: string): string {
  return value === undefined ? '' : requiredString(value, endpoint)
}

function requiredArray(value: unknown, endpoint: string): unknown[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value
}

function parsePlatform(value: unknown, endpoint: string): UpstreamPlatform {
  if (value !== 'newapi' && value !== 'sub2api') invalidResponse(endpoint)
  return value
}

function parseGroup(value: unknown, endpoint: string): UpstreamGroup {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    id: value.id === undefined ? null : requiredId(value.id, endpoint),
    name: requiredString(value.name, endpoint),
    platform: optionalText(value.platform, endpoint),
    ratio: numberText(value.ratio, endpoint, true),
  }
}
function nonNegativeInteger(value: unknown, endpoint: string): number {
  const number = requiredStrictInteger(value, endpoint)
  if (number < 0) invalidResponse(endpoint)
  return number
}

function parseSubscription(
  value: unknown,
  endpoint: string
): UpstreamSubscription {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    id: requiredId(value.id, endpoint),
    upstream_group_id: nonNegativeInteger(value.upstream_group_id, endpoint),
    status: requiredString(value.status, endpoint),
    expires_at: requiredString(value.expires_at, endpoint),
  }
}

function optionalPositive(value: unknown, endpoint: string): number | null {
  if (value === undefined) return null
  const number = requiredStrictNumber(value, endpoint)
  if (number <= 0) invalidResponse(endpoint)
  return number
}

function optionalNonNegative(value: unknown, endpoint: string): number | null {
  if (value === undefined) return null
  const number = requiredStrictNumber(value, endpoint)
  if (number < 0) invalidResponse(endpoint)
  return number
}

function optionalInteger(value: unknown, endpoint: string): number | null {
  return value === undefined ? null : requiredStrictInteger(value, endpoint)
}

function optionalBoolean(value: unknown, endpoint: string): boolean | null {
  return value === undefined ? null : requiredBoolean(value, endpoint)
}

function parseQuotaDisplay(
  value: Record<string, unknown>,
  endpoint: string
): UpstreamQuotaDisplay {
  return {
    quota_per_unit: optionalPositive(value.quota_per_unit, endpoint),
    quota_display_type: optionalText(value.quota_display_type, endpoint),
    usd_exchange_rate: optionalPositive(value.usd_exchange_rate, endpoint),
    custom_currency_symbol: optionalText(
      value.custom_currency_symbol,
      endpoint
    ),
    custom_currency_exchange_rate: optionalPositive(
      value.custom_currency_exchange_rate,
      endpoint
    ),
  }
}

function parseSiteMeta(value: unknown, endpoint: string): UpstreamSiteMeta {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    username: optionalText(value.username, endpoint),
    balance:
      value.balance === undefined ? '' : numberText(value.balance, endpoint),
    upstream_group: optionalText(value.upstream_group, endpoint),
    ...parseQuotaDisplay(value, endpoint),
  }
}

export function parseUpstreamCapabilities(
  value: unknown,
  endpoint: string
): UpstreamCapabilities {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    platform: parsePlatform(value.platform, endpoint),
    version: requiredString(value.version, endpoint),
    site_name: requiredString(value.site_name, endpoint),
    captcha_enabled: requiredBoolean(value.captcha_enabled, endpoint),
    turnstile_enabled: requiredBoolean(value.turnstile_enabled, endpoint),
    password_login_available: requiredBoolean(
      value.password_login_available,
      endpoint
    ),
    totp_enabled: optionalBoolean(value.totp_enabled, endpoint),
    ...parseQuotaDisplay(value, endpoint),
  }
}

export function parseUpstreamAccount(
  value: unknown,
  endpoint = root
): UpstreamAccount {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    id: requiredId(value.id, endpoint),
    name: requiredString(value.name, endpoint),
    platform: parsePlatform(value.platform, endpoint),
    base_url: requiredString(value.base_url, endpoint),
    has_proxy: requiredBoolean(value.has_proxy, endpoint),
    status: requiredString(value.status, endpoint, false),
    last_error: requiredString(value.last_error, endpoint),
    last_sync_at: requiredTime(value.last_sync_at, endpoint),
    next_sync_at: requiredTime(value.next_sync_at, endpoint),
    upstream_groups: requiredArray(value.upstream_groups, endpoint).map(
      (item) => parseGroup(item, endpoint)
    ),
    subscriptions: requiredArray(value.subscriptions, endpoint).map((item) =>
      parseSubscription(item, endpoint)
    ),
    site_meta: parseSiteMeta(value.site_meta, endpoint),
  }
}

export function parseUpstreamRemoteKey(
  value: unknown,
  endpoint: string
): UpstreamRemoteKey {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    id: requiredId(value.id, endpoint),
    name: requiredString(value.name, endpoint),
    remote_key_prefix: requiredString(value.remote_key_prefix, endpoint),
    status: requiredString(value.status, endpoint, false),
    upstream_group: requiredString(value.upstream_group, endpoint),
    upstream_group_id: nonNegativeInteger(value.upstream_group_id, endpoint),
    quota: optionalNonNegative(value.quota, endpoint),
    quota_used: optionalNonNegative(value.quota_used, endpoint),
    expires_at:
      value.expires_at === undefined
        ? null
        : requiredString(value.expires_at, endpoint),
    remain_quota: optionalInteger(value.remain_quota, endpoint),
    used_quota: optionalInteger(value.used_quota, endpoint),
    unlimited_quota: optionalBoolean(value.unlimited_quota, endpoint),
    expired_time: optionalInteger(value.expired_time, endpoint),
  }
}

export function parseUpstreamBinding(
  value: unknown,
  endpoint: string
): UpstreamBinding {
  if (!isRecord(value)) invalidResponse(endpoint)
  const factor = requiredString(value.ratio_sync_factor, endpoint)
  if (!isDecimal(factor)) invalidResponse(endpoint)
  return {
    id: requiredId(value.id, endpoint),
    account_id: requiredId(value.account_id, endpoint),
    remote_key_id: requiredId(value.remote_key_id, endpoint),
    channel_id: requiredId(value.channel_id, endpoint),
    channel_created_time: requiredTime(value.channel_created_time, endpoint),
    stale: requiredBoolean(value.stale, endpoint),
    remote_status: requiredString(value.remote_status, endpoint),
    ratio_sync_enabled: requiredBoolean(value.ratio_sync_enabled, endpoint),
    ratio_sync_factor: factor,
    ratio_sync_error: requiredString(value.ratio_sync_error, endpoint),
  }
}

function parseAuthResult(value: unknown, endpoint: string): UpstreamAuthResult {
  if (!isRecord(value)) invalidResponse(endpoint)
  const keys = Object.keys(value)
  if (keys.length !== 1) invalidResponse(endpoint)
  if (keys[0] === 'account_id')
    return {
      kind: 'account',
      account_id: requiredId(value.account_id, endpoint),
    }
  if (keys[0] === 'challenge')
    return {
      kind: 'challenge',
      challenge: requiredString(value.challenge, endpoint, false),
    }
  invalidResponse(endpoint)
}

function parseChannelResult(value: unknown, endpoint: string): number {
  if (!isRecord(value)) invalidResponse(endpoint)
  return requiredId(value.channel_id, endpoint)
}

function list<T>(
  value: unknown,
  endpoint: string,
  parse: (item: unknown, endpoint: string) => T
): T[] {
  return requiredArray(value, endpoint).map((item) => parse(item, endpoint))
}

const remote = (signal?: AbortSignal) => ({
  signal,
  timeoutMs: REMOTE_TIMEOUT_MS,
})

function accountPath(id: number, suffix = ''): string {
  return `${root}/${positiveId(id, root)}${suffix}`
}

function keyPath(id: number, keyId: number, suffix = ''): string {
  return accountPath(id, `/keys/${positiveId(keyId, root)}${suffix}`)
}

function bindingPath(id: number, channelId: number): string {
  return accountPath(id, `/bindings/${positiveId(channelId, root)}`)
}

async function mutate(
  method: 'post' | 'patch' | 'delete',
  url: string,
  body?: unknown,
  signal?: AbortSignal
): Promise<void> {
  const value = await call(() => {
    if (method === 'delete')
      return api.delete<unknown>(url, undefined, { signal })
    if (method === 'patch') return api.patch<unknown>(url, body, { signal })
    return api.post<unknown>(url, body, remote(signal))
  })
  requireNull(value, url)
}

export const upstreamsApi = {
  async list(signal?: AbortSignal): Promise<UpstreamAccount[]> {
    const value = await call(() =>
      api.get<unknown>(root, undefined, { signal })
    )
    return list(value, root, parseUpstreamAccount)
  },
  async get(id: number, signal?: AbortSignal): Promise<UpstreamAccount> {
    const url = accountPath(id)
    const value = await call(() => api.get<unknown>(url, undefined, { signal }))
    return parseUpstreamAccount(value, url)
  },
  async probe(
    input: { platform: UpstreamPlatform; base_url: string; proxy_url?: string },
    signal?: AbortSignal
  ): Promise<UpstreamCapabilities> {
    const url = `${root}/probe`
    const value = await call(() =>
      api.post<unknown>(url, input, remote(signal))
    )
    return parseUpstreamCapabilities(value, url)
  },
  async auth(
    input: UpstreamAuthInput,
    signal?: AbortSignal
  ): Promise<UpstreamAuthResult> {
    const url = `${root}/auth`
    const value = await call(() =>
      api.post<unknown>(url, input, remote(signal))
    )
    return parseAuthResult(value, url)
  },
  async verify(
    input: { challenge: string; code: string },
    signal?: AbortSignal
  ): Promise<UpstreamAuthResult> {
    const url = `${root}/auth/verify`
    const value = await call(() =>
      api.post<unknown>(url, input, remote(signal))
    )
    return parseAuthResult(value, url)
  },
  update(
    id: number,
    input: { name?: string; enabled?: boolean },
    signal?: AbortSignal
  ) {
    return mutate('patch', accountPath(id), input, signal)
  },
  updateEndpoint(
    id: number,
    input: UpstreamEndpointUpdate,
    signal?: AbortSignal
  ) {
    return mutate('patch', accountPath(id, '/endpoint'), input, signal)
  },
  remove(id: number, signal?: AbortSignal) {
    return mutate('delete', accountPath(id), undefined, signal)
  },
  sync(id: number, signal?: AbortSignal) {
    return mutate('post', accountPath(id, '/sync'), undefined, signal)
  },
  async keys(id: number, signal?: AbortSignal): Promise<UpstreamRemoteKey[]> {
    const url = accountPath(id, '/keys')
    const value = await call(() => api.get<unknown>(url, undefined, { signal }))
    return list(value, url, parseUpstreamRemoteKey)
  },
  async createKey(
    id: number,
    input: UpstreamKeyInput,
    signal?: AbortSignal
  ): Promise<UpstreamRemoteKey> {
    const url = accountPath(id, '/keys')
    const value = await call(() =>
      api.post<unknown>(url, input, remote(signal))
    )
    return parseUpstreamRemoteKey(value, url)
  },
  updateKey(
    id: number,
    keyId: number,
    input: UpstreamKeyInput,
    signal?: AbortSignal
  ) {
    return mutate('patch', keyPath(id, keyId), input, signal)
  },
  setKeyEnabled(
    id: number,
    keyId: number,
    enabled: boolean,
    signal?: AbortSignal
  ) {
    return mutate('patch', keyPath(id, keyId, '/status'), { enabled }, signal)
  },
  removeKey(id: number, keyId: number, signal?: AbortSignal) {
    return mutate('delete', keyPath(id, keyId), undefined, signal)
  },
  async bindings(id: number, signal?: AbortSignal): Promise<UpstreamBinding[]> {
    const url = accountPath(id, '/bindings')
    const value = await call(() => api.get<unknown>(url, undefined, { signal }))
    return list(value, url, parseUpstreamBinding)
  },
  /** Bindings for visible channels; callers pass at most 500 positive IDs. */
  async bindingsForChannels(
    channelIds: number[],
    signal?: AbortSignal
  ): Promise<UpstreamBinding[]> {
    const ids = [...new Set(channelIds)]
    if (!ids.length) return []
    if (ids.length > MAX_BINDING_QUERY_IDS)
      throw new RangeError('Too many channel ids')
    ids.forEach((id) => positiveId(id, root))
    const url = `${root}/bindings`
    const value = await call(() =>
      api.get<unknown>(url, { channel_ids: ids.join(',') }, { signal })
    )
    return list(value, url, parseUpstreamBinding)
  },
  bindChannel(
    id: number,
    keyId: number,
    channelId: number,
    signal?: AbortSignal
  ) {
    return mutate(
      'post',
      keyPath(id, keyId, '/bind'),
      { channel_id: positiveId(channelId, root) },
      signal
    )
  },
  async createChannelFromKey(
    id: number,
    keyId: number,
    input: UpstreamChannelInput,
    signal?: AbortSignal
  ): Promise<number> {
    const url = keyPath(id, keyId, '/channel')
    const value = await call(() =>
      api.post<unknown>(url, input, remote(signal))
    )
    return parseChannelResult(value, url)
  },
  async createKeyAndChannel(
    id: number,
    input: UpstreamChannelInput & { new_key: UpstreamKeyInput },
    signal?: AbortSignal
  ): Promise<number> {
    const url = accountPath(id, '/channels')
    const value = await call(() =>
      api.post<unknown>(url, input, remote(signal))
    )
    return parseChannelResult(value, url)
  },
  updateBinding(
    id: number,
    channelId: number,
    input: UpstreamBindingUpdate,
    signal?: AbortSignal
  ) {
    return mutate('patch', bindingPath(id, channelId), input, signal)
  },
  reverifyBinding(id: number, channelId: number, signal?: AbortSignal) {
    return mutate(
      'post',
      `${bindingPath(id, channelId)}/reverify`,
      undefined,
      signal
    )
  },
  unbind(id: number, channelId: number, signal?: AbortSignal) {
    return mutate('delete', bindingPath(id, channelId), undefined, signal)
  },
}
