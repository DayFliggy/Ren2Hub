// Wire shapes of the top-up discount APIs; see docs/billing/TOPUP_DISCOUNT.md.
// Rates are decimal strings and thresholds integer quota strings, so neither
// passes through a JSON number.

export const TOPUP_DISCOUNT_MODES = ['off', 'shadow', 'on'] as const
export type TopupDiscountMode = (typeof TOPUP_DISCOUNT_MODES)[number]

export const TOPUP_DISCOUNT_WINDOWS = ['day', 'week', 'month'] as const
export type TopupDiscountWindow = (typeof TOPUP_DISCOUNT_WINDOWS)[number]

export interface TopupDiscountTier {
  threshold_quota: string
  rate: string
}

export interface TopupDiscountTable {
  mode: TopupDiscountMode
  tiers: TopupDiscountTier[]
}

export interface TopupDiscountConfig {
  schema_version: 1
  timezone: string
  cap_rate: string
  fixed_rate: string
  amount: TopupDiscountTable
  site: TopupDiscountTable
  personal: Record<TopupDiscountWindow, TopupDiscountTable>
  excluded_user_ids: number[]
}

export interface TopupDiscountSettings {
  version: number
  created_at: number
  created_by: number
  config: TopupDiscountConfig
}

export interface TopupDiscountFailureStat {
  count: number
  /** Unix seconds; 0 when the failure never happened in this process. */
  last_at: number
}

export const TOPUP_DISCOUNT_HEALTH_KEYS = [
  'hourly_write_failures',
  'window_query_failures',
  'cleanup_failures',
] as const
export type TopupDiscountHealthKey = (typeof TOPUP_DISCOUNT_HEALTH_KEYS)[number]

export interface TopupDiscountStatus {
  business_date: string
  cutoff_utc: number
  timezone: string
  config_version: number
  cached_users: number
  site_month_quota: string
  site_tier_rate: string
  earliest_hour_utc: number | null
  health: Record<TopupDiscountHealthKey, TopupDiscountFailureStat>
}

/** A quota-equivalent replay of recent successful top-ups, not a bill. */
export interface TopupDiscountEstimate {
  from_date: string
  to_date: string
  days: number
  orders: number
  unrated_orders: number
  credited_quota: string
  discount_quota: string
  discount_share: string
  capped_quota_share: string
  earliest_hour_utc: number | null
  history_covers_start: boolean
}

export const TOPUP_DISCOUNT_SELF_STATUSES = ['inactive', 'ok'] as const
export type TopupDiscountSelfStatus =
  (typeof TOPUP_DISCOUNT_SELF_STATUSES)[number]

export interface TopupDiscountSelfWindow {
  window: TopupDiscountWindow
  quota: string
  rate: string
  tiers: TopupDiscountTier[]
}

/**
 * The discount of the caller's next top-up before its order-dependent amount
 * tier, which is listed in amount_tiers. Never carries the site total or
 * shadow parts.
 */
export interface TopupDiscountSelf {
  status: TopupDiscountSelfStatus
  factor: string
  fixed_rate: string
  site_rate: string
  personal_rate: string
  raw_rate: string
  applied_rate: string
  cap_rate: string
  cap_applied: boolean
  winner_window?: TopupDiscountWindow
  business_date?: string
  amount_tiers: TopupDiscountTier[]
  windows: TopupDiscountSelfWindow[]
}
