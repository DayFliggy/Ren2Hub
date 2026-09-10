import { api } from '@/api/console'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from '@/api/contracts'

export const MAX_USER_CONCURRENCY = 1_000_000
export const MAX_USER_RPM = 1_000_000_000

export interface AccountClass {
  code: string
  name: string
  default_concurrency: number
  default_rpm: number
  topup_multiplier: number
  policy_version: number
}

export function parseAccountClass(value: unknown): AccountClass {
  const endpoint = '/api/account-classes'
  if (!isRecord(value)) invalidResponse(endpoint)
  const row = {
    code: requiredString(value.code, endpoint, false),
    name: requiredString(value.name, endpoint, false),
    default_concurrency: requiredStrictInteger(
      value.default_concurrency,
      endpoint
    ),
    default_rpm: requiredStrictInteger(value.default_rpm, endpoint),
    topup_multiplier: requiredStrictNumber(value.topup_multiplier, endpoint),
    policy_version: requiredStrictInteger(value.policy_version, endpoint),
  }
  if (
    row.default_concurrency < 0 ||
    row.default_concurrency > MAX_USER_CONCURRENCY ||
    row.default_rpm < 0 ||
    row.default_rpm > MAX_USER_RPM ||
    row.topup_multiplier <= 0 ||
    row.policy_version < 1
  )
    invalidResponse(endpoint)
  return row
}

/** Empty input inherits the class limit; explicit zero remains unlimited. */
export function parseLimitOverride(
  value: string,
  maximum: number
): number | null | undefined {
  if (value.trim() === '') return null
  if (!/^\d+$/.test(value)) return undefined
  const limit = Number(value)
  return Number.isSafeInteger(limit) && limit <= maximum ? limit : undefined
}

export const accountClassesApi = {
  async list(signal?: AbortSignal): Promise<AccountClass[]> {
    const result = await api.get<unknown>('/api/account-classes', undefined, {
      signal,
    })
    if (!Array.isArray(result)) invalidResponse('/api/account-classes')
    return result.map(parseAccountClass)
  },
  create: (input: Omit<AccountClass, 'policy_version'>) =>
    api.post('/api/account-classes', input),
  update: (input: AccountClass) => api.put('/api/account-classes', input),
  remove: (code: string) => api.delete('/api/account-classes', { code }),
}
