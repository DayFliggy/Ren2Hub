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
  TOPUP_DISCOUNT_HEALTH_KEYS,
  TOPUP_DISCOUNT_MODES,
  TOPUP_DISCOUNT_SELF_STATUSES,
  TOPUP_DISCOUNT_WINDOWS,
  type TopupDiscountConfig,
  type TopupDiscountEstimate,
  type TopupDiscountFailureStat,
  type TopupDiscountHealthKey,
  type TopupDiscountSelf,
  type TopupDiscountSelfWindow,
  type TopupDiscountSettings,
  type TopupDiscountStatus,
  type TopupDiscountTable,
  type TopupDiscountTier,
  type TopupDiscountWindow,
} from '@/types/topupDiscount'

const CONFIG_ENDPOINT = '/api/option/topup_discount'
const STATUS_ENDPOINT = '/api/option/topup_discount/status'
const ESTIMATE_ENDPOINT = '/api/option/topup_discount/estimate'
const SELF_ENDPOINT = '/api/user/topup-discount'

// Formats from model/topup_discount_config.go. The server stays authoritative;
// the editor mirrors them so a draft fails before it is sent.
export const RATE_PATTERN = /^(0|[1-9][0-9]*)(\.[0-9]{1,4})?$/
export const THRESHOLD_PATTERN = /^(0|[1-9][0-9]{0,17})$/

export const TOPUP_DISCOUNT_CONFLICT = 'topup_discount_version_conflict'

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

function parseTier(value: unknown, endpoint: string): TopupDiscountTier {
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

function parseTiers(value: unknown, endpoint: string): TopupDiscountTier[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((tier) => parseTier(tier, endpoint))
}

function parseTable(value: unknown, endpoint: string): TopupDiscountTable {
  const table = exactRecord(value, ['mode', 'tiers'], endpoint)
  return {
    mode: oneOf(table.mode, TOPUP_DISCOUNT_MODES, endpoint),
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

export function parseTopupDiscountConfig(
  value: unknown,
  endpoint = CONFIG_ENDPOINT
): TopupDiscountConfig {
  const config = exactRecord(
    value,
    [
      'schema_version',
      'timezone',
      'cap_rate',
      'fixed_rate',
      'amount',
      'site',
      'personal',
      'excluded_user_ids',
    ],
    endpoint
  )
  if (config.schema_version !== 1) invalidResponse(endpoint)
  const personal = exactRecord(
    config.personal,
    TOPUP_DISCOUNT_WINDOWS,
    endpoint
  )
  return {
    schema_version: 1,
    timezone: requiredString(config.timezone, endpoint, false),
    cap_rate: rate(config.cap_rate, endpoint),
    fixed_rate: rate(config.fixed_rate, endpoint),
    amount: parseTable(config.amount, endpoint),
    site: parseTable(config.site, endpoint),
    personal: {
      day: parseTable(personal.day, endpoint),
      week: parseTable(personal.week, endpoint),
      month: parseTable(personal.month, endpoint),
    },
    excluded_user_ids: parseExcluded(config.excluded_user_ids, endpoint),
  }
}

export function parseTopupDiscountSettings(
  value: unknown,
  endpoint = CONFIG_ENDPOINT
): TopupDiscountSettings {
  if (!isRecord(value)) invalidResponse(endpoint)
  const version = requiredStrictInteger(value.version, endpoint)
  if (version < 0) invalidResponse(endpoint)
  return {
    version,
    created_at: requiredStrictInteger(value.created_at, endpoint),
    created_by: requiredStrictInteger(value.created_by, endpoint),
    config: parseTopupDiscountConfig(value.config, endpoint),
  }
}

function parseFailure(
  value: unknown,
  endpoint: string
): TopupDiscountFailureStat {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    count: requiredStrictInteger(value.count, endpoint),
    last_at: requiredStrictInteger(value.last_at, endpoint),
  }
}

export function parseTopupDiscountStatus(value: unknown): TopupDiscountStatus {
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
      TOPUP_DISCOUNT_HEALTH_KEYS.map((key) => [
        key,
        parseFailure(health[key], endpoint),
      ])
    ) as Record<TopupDiscountHealthKey, TopupDiscountFailureStat>,
  }
}

export function parseTopupDiscountEstimate(
  value: unknown
): TopupDiscountEstimate {
  const endpoint = ESTIMATE_ENDPOINT
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    from_date: requiredString(value.from_date, endpoint, false),
    to_date: requiredString(value.to_date, endpoint, false),
    days: requiredStrictInteger(value.days, endpoint),
    orders: requiredStrictInteger(value.orders, endpoint),
    unrated_orders: requiredStrictInteger(value.unrated_orders, endpoint),
    credited_quota: requiredQuota(value.credited_quota, endpoint),
    discount_quota: requiredQuota(value.discount_quota, endpoint),
    discount_share: rate(value.discount_share, endpoint),
    capped_quota_share: rate(value.capped_quota_share, endpoint),
    earliest_hour_utc: nullableInteger(value.earliest_hour_utc, endpoint),
    history_covers_start: requiredBoolean(value.history_covers_start, endpoint),
  }
}

function parseSelfWindow(
  value: unknown,
  endpoint: string
): TopupDiscountSelfWindow {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    window: oneOf(value.window, TOPUP_DISCOUNT_WINDOWS, endpoint),
    quota: requiredQuota(value.quota, endpoint),
    rate: rate(value.rate, endpoint),
    tiers: parseTiers(value.tiers, endpoint),
  }
}

export function parseTopupDiscountSelf(value: unknown): TopupDiscountSelf {
  const endpoint = SELF_ENDPOINT
  if (!isRecord(value) || !Array.isArray(value.windows))
    invalidResponse(endpoint)
  const result: TopupDiscountSelf = {
    status: oneOf(value.status, TOPUP_DISCOUNT_SELF_STATUSES, endpoint),
    factor: rate(value.factor, endpoint),
    fixed_rate: rate(value.fixed_rate, endpoint),
    site_rate: rate(value.site_rate, endpoint),
    personal_rate: rate(value.personal_rate, endpoint),
    raw_rate: rate(value.raw_rate, endpoint),
    applied_rate: rate(value.applied_rate, endpoint),
    cap_rate: rate(value.cap_rate, endpoint),
    cap_applied: requiredBoolean(value.cap_applied, endpoint),
    amount_tiers: parseTiers(value.amount_tiers, endpoint),
    windows: value.windows.map((window) => parseSelfWindow(window, endpoint)),
  }
  // Both are omitted when empty.
  if (value.winner_window !== undefined)
    result.winner_window = oneOf<TopupDiscountWindow>(
      value.winner_window,
      TOPUP_DISCOUNT_WINDOWS,
      endpoint
    )
  if (value.business_date !== undefined)
    result.business_date = requiredString(value.business_date, endpoint, false)
  return result
}

/** The save endpoint answers a stale expected_version with this 409. */
export function isTopupDiscountConflict(cause: unknown): boolean {
  return (
    cause instanceof ApiError &&
    cause.status === 409 &&
    cause.code === TOPUP_DISCOUNT_CONFLICT
  )
}

export const topupDiscountApi = {
  async load(signal?: AbortSignal) {
    return parseTopupDiscountSettings(
      await api.get(CONFIG_ENDPOINT, undefined, { signal })
    )
  },
  async save(expectedVersion: number, config: TopupDiscountConfig) {
    return parseTopupDiscountSettings(
      await api.patch(CONFIG_ENDPOINT, {
        expected_version: expectedVersion,
        config,
      })
    )
  },
  async status(signal?: AbortSignal) {
    return parseTopupDiscountStatus(
      await api.get(STATUS_ENDPOINT, undefined, { signal })
    )
  },
  async estimate(config: TopupDiscountConfig, signal?: AbortSignal) {
    return parseTopupDiscountEstimate(
      await api.post(ESTIMATE_ENDPOINT, { config }, { signal })
    )
  },
  async self(signal?: AbortSignal) {
    return parseTopupDiscountSelf(
      await api.get(SELF_ENDPOINT, undefined, { signal })
    )
  },
}
