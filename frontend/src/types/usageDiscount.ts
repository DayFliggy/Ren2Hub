// Wire shapes of the usage discount APIs; see docs/billing/USAGE_DISCOUNT.md.
// Rates are decimal strings and thresholds integer quota strings, so neither
// passes through a JSON number.

export const USAGE_DISCOUNT_MODES = ['off', 'shadow', 'on'] as const
export type UsageDiscountMode = (typeof USAGE_DISCOUNT_MODES)[number]

export const USAGE_DISCOUNT_WINDOWS = ['day', 'week', 'month'] as const
export type UsageDiscountWindow = (typeof USAGE_DISCOUNT_WINDOWS)[number]

export interface UsageDiscountTier {
  threshold_quota: string
  rate: string
}

export interface UsageDiscountTable {
  mode: UsageDiscountMode
  tiers: UsageDiscountTier[]
}

export interface UsageDiscountConfig {
  schema_version: 1
  timezone: string
  cap_rate: string
  fixed_rate: string
  site: UsageDiscountTable
  personal: Record<UsageDiscountWindow, UsageDiscountTable>
  excluded_user_ids: number[]
}

export interface UsageDiscountSettings {
  version: number
  created_at: number
  created_by: number
  config: UsageDiscountConfig
}

export interface UsageDiscountFailureStat {
  count: number
  /** Unix seconds; 0 when the failure never happened in this process. */
  last_at: number
}

export const USAGE_DISCOUNT_HEALTH_KEYS = [
  'hourly_write_failures',
  'window_query_failures',
  'cleanup_failures',
] as const
export type UsageDiscountHealthKey = (typeof USAGE_DISCOUNT_HEALTH_KEYS)[number]

export interface UsageDiscountStatus {
  business_date: string
  cutoff_utc: number
  timezone: string
  config_version: number
  cached_users: number
  site_month_quota: string
  site_tier_rate: string
  earliest_hour_utc: number | null
  health: Record<UsageDiscountHealthKey, UsageDiscountFailureStat>
}

/** A replay of recent hourly data, not a bill. */
export interface UsageDiscountEstimate {
  from_date: string
  to_date: string
  days: number
  revenue_quota: string
  discount_quota: string
  discount_share: string
  capped_revenue_share: string
  earliest_hour_utc: number | null
  history_complete: boolean
}

export const USAGE_DISCOUNT_SELF_STATUSES = [
  'inactive',
  'ok',
  'unavailable',
  'not_applicable',
] as const
export type UsageDiscountSelfStatus =
  (typeof USAGE_DISCOUNT_SELF_STATUSES)[number]

export interface UsageDiscountSelfWindow {
  window: UsageDiscountWindow
  quota: string
  rate: string
  tiers: UsageDiscountTier[]
}

/** The caller's own discount; never carries the site total or shadow parts. */
export interface UsageDiscountSelf {
  status: UsageDiscountSelfStatus
  factor: string
  global_factor: string
  fixed_rate: string
  site_rate: string
  applied_rate: string
  cap_rate: string
  cap_applied: boolean
  winner_window?: UsageDiscountWindow
  business_date?: string
  windows: UsageDiscountSelfWindow[]
}
