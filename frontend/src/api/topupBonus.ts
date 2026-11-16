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
  TOPUP_BONUS_HEALTH_KEYS,
  TOPUP_BONUS_MODES,
  TOPUP_BONUS_SELF_STATUSES,
  TOPUP_BONUS_SOURCES,
  TOPUP_BONUS_WINDOWS,
  type TopupBonusConfig,
  type TopupBonusEstimate,
  type TopupBonusEstimateSource,
  type TopupBonusFailureStat,
  type TopupBonusHealthKey,
  type TopupBonusQuote,
  type TopupBonusSelf,
  type TopupBonusSelfWindow,
  type TopupBonusSettings,
  type TopupBonusStatus,
  type TopupBonusTable,
  type TopupBonusTier,
  type TopupBonusWindow,
} from '@/types/topupBonus'

const CONFIG_ENDPOINT = '/api/option/topup_bonus'
const STATUS_ENDPOINT = '/api/option/topup_bonus/status'
const ESTIMATE_ENDPOINT = '/api/option/topup_bonus/estimate'
const SELF_ENDPOINT = '/api/user/topup-bonus'
const PREVIEW_ENDPOINT = '/api/user/topup-bonus/preview'

// Formats from model/topup_bonus_config.go. The server stays authoritative;
// the editor mirrors them so a draft fails before it is sent.
export const RATE_PATTERN = /^(0|[1-9][0-9]*)(\.[0-9]{1,4})?$/
export const THRESHOLD_PATTERN = /^(0|[1-9][0-9]{0,17})$/

export const TOPUP_BONUS_CONFLICT = 'topup_bonus_version_conflict'

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

function parseTier(value: unknown, endpoint: string): TopupBonusTier {
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

function parseTiers(value: unknown, endpoint: string): TopupBonusTier[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((tier) => parseTier(tier, endpoint))
}

function parseTable(value: unknown, endpoint: string): TopupBonusTable {
  const table = exactRecord(value, ['mode', 'tiers'], endpoint)
  return {
    mode: oneOf(table.mode, TOPUP_BONUS_MODES, endpoint),
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

export function parseTopupBonusConfig(
  value: unknown,
  endpoint = CONFIG_ENDPOINT
): TopupBonusConfig {
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
  const personal = exactRecord(config.personal, TOPUP_BONUS_WINDOWS, endpoint)
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

export function parseTopupBonusSettings(
  value: unknown,
  endpoint = CONFIG_ENDPOINT
): TopupBonusSettings {
  if (!isRecord(value)) invalidResponse(endpoint)
  const version = requiredStrictInteger(value.version, endpoint)
  if (version < 0) invalidResponse(endpoint)
  return {
    version,
    created_at: requiredStrictInteger(value.created_at, endpoint),
    created_by: requiredStrictInteger(value.created_by, endpoint),
    config: parseTopupBonusConfig(value.config, endpoint),
  }
}

function parseFailure(value: unknown, endpoint: string): TopupBonusFailureStat {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    count: requiredStrictInteger(value.count, endpoint),
    last_at: requiredStrictInteger(value.last_at, endpoint),
  }
}

export function parseTopupBonusStatus(value: unknown): TopupBonusStatus {
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
      TOPUP_BONUS_HEALTH_KEYS.map((key) => [
        key,
        parseFailure(health[key], endpoint),
      ])
    ) as Record<TopupBonusHealthKey, TopupBonusFailureStat>,
  }
}

function parseEstimateSource(
  value: unknown,
  source: TopupBonusEstimateSource['source'],
  endpoint: string
): TopupBonusEstimateSource {
  if (!isRecord(value) || value.source !== source) invalidResponse(endpoint)
  return {
    source,
    records: requiredStrictInteger(value.records, endpoint),
    principal_quota: requiredQuota(value.principal_quota, endpoint),
    bonus_quota: requiredQuota(value.bonus_quota, endpoint),
  }
}

export function parseTopupBonusEstimate(value: unknown): TopupBonusEstimate {
  const endpoint = ESTIMATE_ENDPOINT
  if (
    !isRecord(value) ||
    !Array.isArray(value.sources) ||
    value.sources.length !== TOPUP_BONUS_SOURCES.length
  )
    invalidResponse(endpoint)
  const sources = value.sources
  return {
    from_date: requiredString(value.from_date, endpoint, false),
    to_date: requiredString(value.to_date, endpoint, false),
    days: requiredStrictInteger(value.days, endpoint),
    records: requiredStrictInteger(value.records, endpoint),
    unrated_records: requiredStrictInteger(value.unrated_records, endpoint),
    principal_quota: requiredQuota(value.principal_quota, endpoint),
    bonus_quota: requiredQuota(value.bonus_quota, endpoint),
    bonus_share: rate(value.bonus_share, endpoint),
    capped_principal_share: rate(value.capped_principal_share, endpoint),
    // The server lists every source once, in this order.
    sources: TOPUP_BONUS_SOURCES.map((source, index) =>
      parseEstimateSource(sources[index], source, endpoint)
    ),
    earliest_hour_utc: nullableInteger(value.earliest_hour_utc, endpoint),
    history_covers_start: requiredBoolean(value.history_covers_start, endpoint),
  }
}

function parseSelfWindow(
  value: unknown,
  endpoint: string
): TopupBonusSelfWindow {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    window: oneOf(value.window, TOPUP_BONUS_WINDOWS, endpoint),
    quota: requiredQuota(value.quota, endpoint),
    rate: rate(value.rate, endpoint),
    tiers: parseTiers(value.tiers, endpoint),
  }
}

export function parseTopupBonusSelf(value: unknown): TopupBonusSelf {
  const endpoint = SELF_ENDPOINT
  if (!isRecord(value) || !Array.isArray(value.windows))
    invalidResponse(endpoint)
  const result: TopupBonusSelf = {
    status: oneOf(value.status, TOPUP_BONUS_SELF_STATUSES, endpoint),
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
    result.winner_window = oneOf<TopupBonusWindow>(
      value.winner_window,
      TOPUP_BONUS_WINDOWS,
      endpoint
    )
  if (value.business_date !== undefined)
    result.business_date = requiredString(value.business_date, endpoint, false)
  return result
}

/**
 * One bonus quote; principal and bonus are positive and non-negative whole
 * quota, and expected_quota is their sum.
 */
export function parseTopupBonusQuote(
  value: unknown,
  endpoint = PREVIEW_ENDPOINT
): TopupBonusQuote {
  if (!isRecord(value)) invalidResponse(endpoint)
  const principal = requiredQuota(value.principal_quota, endpoint)
  const bonus = requiredQuota(value.bonus_quota, endpoint)
  const expected = requiredQuota(value.expected_quota, endpoint)
  if (
    BigInt(principal) <= 0n ||
    BigInt(bonus) < 0n ||
    BigInt(expected) !== BigInt(principal) + BigInt(bonus)
  )
    invalidResponse(endpoint)
  const result: TopupBonusQuote = {
    principal_quota: principal,
    bonus_quota: bonus,
    expected_quota: expected,
    applied_rate: rate(value.applied_rate, endpoint),
    amount_rate: rate(value.amount_rate, endpoint),
    fixed_rate: rate(value.fixed_rate, endpoint),
    site_rate: rate(value.site_rate, endpoint),
    personal_rate: rate(value.personal_rate, endpoint),
    cap_applied: requiredBoolean(value.cap_applied, endpoint),
  }
  if (value.winner_window !== undefined)
    result.winner_window = oneOf<TopupBonusWindow>(
      value.winner_window,
      TOPUP_BONUS_WINDOWS,
      endpoint
    )
  return result
}

/** The save endpoint answers a stale expected_version with this 409. */
export function isTopupBonusConflict(cause: unknown): boolean {
  return (
    cause instanceof ApiError &&
    cause.status === 409 &&
    cause.code === TOPUP_BONUS_CONFLICT
  )
}

export const topupBonusApi = {
  async load(signal?: AbortSignal) {
    return parseTopupBonusSettings(
      await api.get(CONFIG_ENDPOINT, undefined, { signal })
    )
  },
  async save(expectedVersion: number, config: TopupBonusConfig) {
    return parseTopupBonusSettings(
      await api.patch(CONFIG_ENDPOINT, {
        expected_version: expectedVersion,
        config,
      })
    )
  },
  async status(signal?: AbortSignal) {
    return parseTopupBonusStatus(
      await api.get(STATUS_ENDPOINT, undefined, { signal })
    )
  },
  async estimate(config: TopupBonusConfig, signal?: AbortSignal) {
    return parseTopupBonusEstimate(
      await api.post(ESTIMATE_ENDPOINT, { config }, { signal })
    )
  },
  async self(signal?: AbortSignal) {
    return parseTopupBonusSelf(
      await api.get(SELF_ENDPOINT, undefined, { signal })
    )
  },
  /** The bonus of a provider-priced top-up (Stripe, Creem) of principal. */
  async preview(principalQuota: string, signal?: AbortSignal) {
    return parseTopupBonusQuote(
      await api.get(
        PREVIEW_ENDPOINT,
        { principal_quota: principalQuota },
        { signal }
      )
    )
  },
}
