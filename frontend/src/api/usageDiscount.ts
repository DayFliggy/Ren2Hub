import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredQuota,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { ApiError } from './types'
import {
  USAGE_DISCOUNT_HEALTH_KEYS,
  USAGE_DISCOUNT_MODES,
  USAGE_DISCOUNT_SELF_STATUSES,
  USAGE_DISCOUNT_WINDOWS,
  type UsageDiscountConfig,
  type UsageDiscountEstimate,
  type UsageDiscountFailureStat,
  type UsageDiscountHealthKey,
  type UsageDiscountSelf,
  type UsageDiscountSelfWindow,
  type UsageDiscountSettings,
  type UsageDiscountStatus,
  type UsageDiscountTable,
  type UsageDiscountTier,
  type UsageDiscountWindow,
} from '@/types/usageDiscount'

const CONFIG_ENDPOINT = '/api/option/usage_discount'
const STATUS_ENDPOINT = '/api/option/usage_discount/status'
const ESTIMATE_ENDPOINT = '/api/option/usage_discount/estimate'
const SELF_ENDPOINT = '/api/user/usage-discount'

// Formats from model/usage_discount_config.go. The server stays authoritative;
// the editor mirrors them so a draft fails before it is sent.
export const RATE_PATTERN = /^(0|[1-9][0-9]*)(\.[0-9]{1,4})?$/
export const THRESHOLD_PATTERN = /^(0|[1-9][0-9]{0,17})$/

export const USAGE_DISCOUNT_CONFLICT = 'usage_discount_version_conflict'

function rate(value: unknown, endpoint: string): string {
  if (typeof value !== 'string' || !RATE_PATTERN.test(value))
    invalidResponse(endpoint)
  return value
}

function oneOf<T extends string>(
  value: unknown,
  allowed: readonly T[],
  endpoint: string
): T {
  if (!(allowed as readonly unknown[]).includes(value))
    invalidResponse(endpoint)
  return value as T
}

function nullableInteger(value: unknown, endpoint: string): number | null {
  return value === null ? null : requiredStrictInteger(value, endpoint)
}

// The editor saves what it loaded, so a field it does not know would be lost
// on the next save. Config objects must carry exactly the known keys.
function exactRecord(
  value: unknown,
  keys: readonly string[],
  endpoint: string
): Record<string, unknown> {
  if (!isRecord(value)) invalidResponse(endpoint)
  const actual = Object.keys(value)
  if (
    actual.length !== keys.length ||
    actual.some((key) => !keys.includes(key))
  )
    invalidResponse(endpoint)
  return value
}

function parseTier(value: unknown, endpoint: string): UsageDiscountTier {
  const tier = exactRecord(value, ['threshold_quota', 'rate'], endpoint)
  if (
    typeof tier.threshold_quota !== 'string' ||
    !THRESHOLD_PATTERN.test(tier.threshold_quota)
  )
    invalidResponse(endpoint)
  return {
    threshold_quota: tier.threshold_quota,
    rate: rate(tier.rate, endpoint),
  }
}

function parseTiers(value: unknown, endpoint: string): UsageDiscountTier[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((tier) => parseTier(tier, endpoint))
}

function parseTable(value: unknown, endpoint: string): UsageDiscountTable {
  const table = exactRecord(value, ['mode', 'tiers'], endpoint)
  return {
    mode: oneOf(table.mode, USAGE_DISCOUNT_MODES, endpoint),
    tiers: parseTiers(table.tiers, endpoint),
  }
}

function parseExcluded(value: unknown, endpoint: string): number[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((id) => {
    const parsed = requiredStrictInteger(id, endpoint)
    if (parsed <= 0) invalidResponse(endpoint)
    return parsed
  })
}

export function parseUsageDiscountConfig(
  value: unknown,
  endpoint = CONFIG_ENDPOINT
): UsageDiscountConfig {
  const config = exactRecord(
    value,
    [
      'schema_version',
      'timezone',
      'cap_rate',
      'fixed_rate',
      'site',
      'personal',
      'excluded_user_ids',
    ],
    endpoint
  )
  if (config.schema_version !== 1) invalidResponse(endpoint)
  const personal = exactRecord(
    config.personal,
    USAGE_DISCOUNT_WINDOWS,
    endpoint
  )
  return {
    schema_version: 1,
    timezone: requiredString(config.timezone, endpoint, false),
    cap_rate: rate(config.cap_rate, endpoint),
    fixed_rate: rate(config.fixed_rate, endpoint),
    site: parseTable(config.site, endpoint),
    personal: {
      day: parseTable(personal.day, endpoint),
      week: parseTable(personal.week, endpoint),
      month: parseTable(personal.month, endpoint),
    },
    excluded_user_ids: parseExcluded(config.excluded_user_ids, endpoint),
  }
}

export function parseUsageDiscountSettings(
  value: unknown,
  endpoint = CONFIG_ENDPOINT
): UsageDiscountSettings {
  if (!isRecord(value)) invalidResponse(endpoint)
  const version = requiredStrictInteger(value.version, endpoint)
  if (version < 0) invalidResponse(endpoint)
  return {
    version,
    created_at: requiredStrictInteger(value.created_at, endpoint),
    created_by: requiredStrictInteger(value.created_by, endpoint),
    config: parseUsageDiscountConfig(value.config, endpoint),
  }
}

function parseFailure(
  value: unknown,
  endpoint: string
): UsageDiscountFailureStat {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    count: requiredStrictInteger(value.count, endpoint),
    last_at: requiredStrictInteger(value.last_at, endpoint),
  }
}

export function parseUsageDiscountStatus(value: unknown): UsageDiscountStatus {
  const endpoint = STATUS_ENDPOINT
  if (!isRecord(value) || !isRecord(value.health)) invalidResponse(endpoint)
  const health = value.health
  return {
    business_date: requiredString(value.business_date, endpoint, false),
    cutoff_utc: requiredStrictInteger(value.cutoff_utc, endpoint),
    timezone: requiredString(value.timezone, endpoint, false),
    config_version: requiredStrictInteger(value.config_version, endpoint),
    cached_users: requiredStrictInteger(value.cached_users, endpoint),
    site_month_quota: requiredQuota(value.site_month_quota, endpoint),
    site_tier_rate: rate(value.site_tier_rate, endpoint),
    earliest_hour_utc: nullableInteger(value.earliest_hour_utc, endpoint),
    health: Object.fromEntries(
      USAGE_DISCOUNT_HEALTH_KEYS.map((key) => [
        key,
        parseFailure(health[key], endpoint),
      ])
    ) as Record<UsageDiscountHealthKey, UsageDiscountFailureStat>,
  }
}

export function parseUsageDiscountEstimate(
  value: unknown
): UsageDiscountEstimate {
  const endpoint = ESTIMATE_ENDPOINT
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    from_date: requiredString(value.from_date, endpoint, false),
    to_date: requiredString(value.to_date, endpoint, false),
    days: requiredStrictInteger(value.days, endpoint),
    revenue_quota: requiredQuota(value.revenue_quota, endpoint),
    discount_quota: requiredQuota(value.discount_quota, endpoint),
    discount_share: rate(value.discount_share, endpoint),
    capped_revenue_share: rate(value.capped_revenue_share, endpoint),
    earliest_hour_utc: nullableInteger(value.earliest_hour_utc, endpoint),
    history_complete: requiredBoolean(value.history_complete, endpoint),
  }
}

function parseSelfWindow(
  value: unknown,
  endpoint: string
): UsageDiscountSelfWindow {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    window: oneOf(value.window, USAGE_DISCOUNT_WINDOWS, endpoint),
    quota: requiredQuota(value.quota, endpoint),
    rate: rate(value.rate, endpoint),
    tiers: parseTiers(value.tiers, endpoint),
  }
}

export function parseUsageDiscountSelf(value: unknown): UsageDiscountSelf {
  const endpoint = SELF_ENDPOINT
  if (!isRecord(value) || !Array.isArray(value.windows))
    invalidResponse(endpoint)
  const result: UsageDiscountSelf = {
    status: oneOf(value.status, USAGE_DISCOUNT_SELF_STATUSES, endpoint),
    factor: rate(value.factor, endpoint),
    global_factor: rate(value.global_factor, endpoint),
    fixed_rate: rate(value.fixed_rate, endpoint),
    site_rate: rate(value.site_rate, endpoint),
    applied_rate: rate(value.applied_rate, endpoint),
    cap_rate: rate(value.cap_rate, endpoint),
    cap_applied: requiredBoolean(value.cap_applied, endpoint),
    windows: value.windows.map((window) => parseSelfWindow(window, endpoint)),
  }
  // Both are omitted when empty.
  if (value.winner_window !== undefined)
    result.winner_window = oneOf<UsageDiscountWindow>(
      value.winner_window,
      USAGE_DISCOUNT_WINDOWS,
      endpoint
    )
  if (value.business_date !== undefined)
    result.business_date = requiredString(value.business_date, endpoint, false)
  return result
}

/** The save endpoint answers a stale expected_version with this 409. */
export function isUsageDiscountConflict(cause: unknown): boolean {
  return (
    cause instanceof ApiError &&
    cause.status === 409 &&
    cause.code === USAGE_DISCOUNT_CONFLICT
  )
}

export const usageDiscountApi = {
  async load(signal?: AbortSignal) {
    return parseUsageDiscountSettings(
      await api.get(CONFIG_ENDPOINT, undefined, { signal })
    )
  },
  async save(expectedVersion: number, config: UsageDiscountConfig) {
    return parseUsageDiscountSettings(
      await api.patch(CONFIG_ENDPOINT, {
        expected_version: expectedVersion,
        config,
      })
    )
  },
  async status(signal?: AbortSignal) {
    return parseUsageDiscountStatus(
      await api.get(STATUS_ENDPOINT, undefined, { signal })
    )
  },
  async estimate(config: UsageDiscountConfig, signal?: AbortSignal) {
    return parseUsageDiscountEstimate(
      await api.post(ESTIMATE_ENDPOINT, { config }, { signal })
    )
  },
  async self(signal?: AbortSignal) {
    return parseUsageDiscountSelf(
      await api.get(SELF_ENDPOINT, undefined, { signal })
    )
  },
}
