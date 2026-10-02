import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredString,
} from './contracts'

export type ProviderUsageState = 'known' | 'unknown' | 'stale' | 'error'
export interface ProviderUsageWindow {
  name:
    | 'rate_limit_primary'
    | 'rate_limit_secondary'
    | 'code_review_primary'
    | 'code_review_secondary'
  used_percent: number | null
  limit_seconds: number | null
  reset_at: number | null
}
export interface ProviderUsageSnapshot {
  channel_id: number
  provider: 'codex'
  state: ProviderUsageState
  plan_type: string
  windows: ProviderUsageWindow[]
  fetched_at: number
  expires_at: number
  last_attempt_at: number
  next_refresh_at: number
  upstream_status: number
  error_code:
    | ''
    | 'upstream_unavailable'
    | 'upstream_rejected'
    | 'rate_limited'
    | 'credential_refresh_failed'
  auto_refresh: boolean
}

const contract = '/api/channel/:id/provider-usage'
const states = ['known', 'unknown', 'stale', 'error']
const windowNames = [
  'rate_limit_primary',
  'rate_limit_secondary',
  'code_review_primary',
  'code_review_secondary',
]
const errors = [
  '',
  'upstream_unavailable',
  'upstream_rejected',
  'rate_limited',
  'credential_refresh_failed',
]

function integer(value: unknown, maximum = 253402300799, minimum = 0): number {
  const result = requiredStrictInteger(value, contract)
  if (result < minimum || result > maximum) invalidResponse(contract)
  return result
}

export function parseProviderUsage(value: unknown): ProviderUsageSnapshot {
  if (
    !isRecord(value) ||
    value.provider !== 'codex' ||
    !states.includes(String(value.state)) ||
    !Array.isArray(value.windows) ||
    value.windows.length > 4 ||
    typeof value.auto_refresh !== 'boolean'
  )
    invalidResponse(contract)
  const plan = requiredString(value.plan_type, contract)
  if (plan && !/^[a-zA-Z0-9_-]{1,64}$/.test(plan)) invalidResponse(contract)
  if (!errors.includes(String(value.error_code))) invalidResponse(contract)
  const seen = new Set<string>()
  const windows: ProviderUsageWindow[] = value.windows.map((entry) => {
    if (
      !isRecord(entry) ||
      !windowNames.includes(String(entry.name)) ||
      seen.has(String(entry.name))
    )
      invalidResponse(contract)
    seen.add(String(entry.name))
    if (
      entry.used_percent !== null &&
      (typeof entry.used_percent !== 'number' ||
        !Number.isFinite(entry.used_percent) ||
        entry.used_percent < 0 ||
        entry.used_percent > 100)
    )
      invalidResponse(contract)
    return {
      name: entry.name as ProviderUsageWindow['name'],
      used_percent: entry.used_percent as number | null,
      limit_seconds:
        entry.limit_seconds === null
          ? null
          : integer(entry.limit_seconds, 366 * 86400, 1),
      reset_at: entry.reset_at === null ? null : integer(entry.reset_at),
    }
  })
  return {
    channel_id: integer(value.channel_id, 2147483647, 1),
    provider: 'codex',
    state: value.state as ProviderUsageState,
    plan_type: plan,
    windows,
    fetched_at: integer(value.fetched_at),
    expires_at: integer(value.expires_at),
    last_attempt_at: integer(value.last_attempt_at),
    next_refresh_at: integer(value.next_refresh_at),
    upstream_status: integer(value.upstream_status, 599),
    error_code: value.error_code as ProviderUsageSnapshot['error_code'],
    auto_refresh: value.auto_refresh,
  }
}

export const providerUsageApi = {
  async get(channelID: number, signal?: AbortSignal) {
    return parseProviderUsage(
      await api.get<unknown>(
        `/api/channel/${channelID}/provider-usage`,
        undefined,
        { signal }
      )
    )
  },
  async refresh(channelID: number, signal?: AbortSignal) {
    return parseProviderUsage(
      await api.post<unknown>(
        `/api/channel/${channelID}/provider-usage/refresh`,
        undefined,
        { signal }
      )
    )
  },
  async settings(
    channelID: number,
    autoRefresh: boolean,
    signal?: AbortSignal
  ) {
    return parseProviderUsage(
      await api.put<unknown>(
        `/api/channel/${channelID}/provider-usage/settings`,
        { auto_refresh: autoRefresh },
        { signal }
      )
    )
  },
}
