import {
  USAGE_DISCOUNT_WINDOWS,
  type UsageDiscountConfig,
  type UsageDiscountMode,
  type UsageDiscountTable,
  type UsageDiscountTier,
} from '@/types/usageDiscount'
import { compareDecimal } from '@/utils/decimal'
import {
  amountToQuota,
  percentToRate,
  quotaToAmount,
  rateToPercent,
} from './amount'

// The editable form of a usage discount configuration, and its client-side
// check. The rules mirror model/usage_discount_config.go; the server repeats
// them and stays authoritative.

export const MAX_CAP_RATE = '0.5'
export const MAX_TIERS = 10
export const MAX_EXCLUDED = 100

export const TABLE_KEYS = ['site', ...USAGE_DISCOUNT_WINDOWS] as const
export type TableKey = (typeof TABLE_KEYS)[number]

export interface TierDraft {
  /** USD text as typed. */
  amount: string
  /** Percent text as typed; "1.5" is a 1.5% discount. */
  percent: string
  /** The loaded threshold, kept while its amount text is unchanged. */
  loadedQuota?: string
}

export interface TableDraft {
  mode: UsageDiscountMode
  tiers: TierDraft[]
}

export interface UsageDiscountDraft {
  /** Not editable here; saved back as loaded. */
  timezone: string
  cap: string
  fixed: string
  tables: Record<TableKey, TableDraft>
  /** User ids separated by commas or whitespace. */
  excluded: string
}

/** An i18n key under the page messages, with its interpolation values. */
export interface DraftIssue {
  key: string
  params?: Record<string, string | number>
}

/**
 * Field paths: cap, fixed, excluded, `<table>` for the whole table, and
 * `<table>.<row>.amount` / `<table>.<row>.percent` for one tier.
 */
export type DraftErrors = Record<string, DraftIssue>

export type DraftResult =
  { ok: true; config: UsageDiscountConfig } | { ok: false; errors: DraftErrors }

const PERCENT_PATTERN = /^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$/

function tableDraft(table: UsageDiscountTable): TableDraft {
  return {
    mode: table.mode,
    tiers: table.tiers.map((tier) => ({
      amount: quotaToAmount(tier.threshold_quota),
      percent: rateToPercent(tier.rate),
      loadedQuota: tier.threshold_quota,
    })),
  }
}

export function draftFromConfig(
  config: UsageDiscountConfig
): UsageDiscountDraft {
  return {
    timezone: config.timezone,
    cap: rateToPercent(config.cap_rate),
    fixed: rateToPercent(config.fixed_rate),
    tables: {
      site: tableDraft(config.site),
      day: tableDraft(config.personal.day),
      week: tableDraft(config.personal.week),
      month: tableDraft(config.personal.month),
    },
    excluded: config.excluded_user_ids.join(', '),
  }
}

/** A stable identity of the draft text, for dirty and stale checks. */
export function draftKey(draft: UsageDiscountDraft): string {
  return JSON.stringify(draft)
}

/**
 * Percent text as a rate. `max` is the inclusive limit; it is null when the
 * limit itself is invalid and the comparison cannot be made.
 */
function readRate(
  text: string,
  path: string,
  max: string | null,
  errors: DraftErrors
): string | null {
  const percent = text.trim()
  if (!PERCENT_PATTERN.test(percent)) {
    errors[path] = { key: 'errors.percentInvalid' }
    return null
  }
  // Two percent decimals are the four rate decimals the server accepts.
  const rate = percentToRate(percent)
  if (max !== null && compareDecimal(rate, max) > 0) {
    errors[path] = {
      key: path === 'cap' ? 'errors.capTooHigh' : 'errors.overCap',
      params: { max: rateToPercent(max) },
    }
    return null
  }
  return rate
}

function readThreshold(
  tier: TierDraft,
  path: string,
  errors: DraftErrors
): string | null {
  // A loaded threshold may not be expressible as USD text exactly under a
  // custom quota_per_unit; an untouched row keeps the loaded value.
  if (tier.loadedQuota && tier.amount === quotaToAmount(tier.loadedQuota))
    return tier.loadedQuota
  try {
    return amountToQuota(tier.amount)
  } catch (cause) {
    errors[path] = {
      key: cause instanceof Error ? cause.message : 'errors.amountInvalid',
    }
    return null
  }
}

function readTable(
  key: TableKey,
  table: TableDraft,
  cap: string | null,
  errors: DraftErrors
): UsageDiscountTable {
  if (table.tiers.length > MAX_TIERS)
    errors[key] = { key: 'errors.tooManyTiers', params: { max: MAX_TIERS } }
  const tiers: UsageDiscountTier[] = []
  // The last valid value of the row above, for the ordering rules.
  let previousQuota: bigint | null = null
  let previousRate: string | null = null
  for (const [row, tier] of table.tiers.entries()) {
    const amountPath = `${key}.${row}.amount`
    const percentPath = `${key}.${row}.percent`
    const quota = readThreshold(tier, amountPath, errors)
    const rate = readRate(tier.percent, percentPath, cap, errors)
    const threshold = quota === null ? null : BigInt(quota)
    if (
      threshold !== null &&
      previousQuota !== null &&
      threshold <= previousQuota
    )
      errors[amountPath] = { key: 'errors.thresholdOrder' }
    if (
      rate !== null &&
      previousRate !== null &&
      compareDecimal(rate, previousRate) < 0
    )
      errors[percentPath] = { key: 'errors.rateOrder' }
    previousQuota = threshold
    previousRate = rate
    if (quota !== null && rate !== null)
      tiers.push({ threshold_quota: quota, rate })
  }
  return { mode: table.mode, tiers }
}

function readExcluded(text: string, errors: DraftErrors): number[] | null {
  const tokens = text.split(/[\s,，]+/).filter(Boolean)
  if (tokens.length > MAX_EXCLUDED) {
    errors.excluded = {
      key: 'errors.tooManyExcluded',
      params: { max: MAX_EXCLUDED },
    }
    return null
  }
  const ids: number[] = []
  for (const token of tokens) {
    const id = Number(token)
    if (!/^[1-9][0-9]*$/.test(token) || !Number.isSafeInteger(id)) {
      errors.excluded = { key: 'errors.excludedInvalid', params: { id: token } }
      return null
    }
    if (ids.includes(id)) {
      errors.excluded = {
        key: 'errors.excludedDuplicate',
        params: { id: token },
      }
      return null
    }
    ids.push(id)
  }
  return ids
}

/** The configuration a draft describes, or every rule it breaks. */
export function buildConfig(draft: UsageDiscountDraft): DraftResult {
  const errors: DraftErrors = {}
  const cap = readRate(draft.cap, 'cap', MAX_CAP_RATE, errors)
  const fixed = readRate(draft.fixed, 'fixed', cap, errors)
  const tables = Object.fromEntries(
    TABLE_KEYS.map((key) => [
      key,
      readTable(key, draft.tables[key], cap, errors),
    ])
  ) as Record<TableKey, UsageDiscountTable>
  const excluded = readExcluded(draft.excluded, errors)
  if (
    cap === null ||
    fixed === null ||
    excluded === null ||
    Object.keys(errors).length
  )
    return { ok: false, errors }
  return {
    ok: true,
    config: {
      schema_version: 1,
      timezone: draft.timezone,
      cap_rate: cap,
      fixed_rate: fixed,
      site: tables.site,
      personal: { day: tables.day, week: tables.week, month: tables.month },
      excluded_user_ids: excluded,
    },
  }
}
