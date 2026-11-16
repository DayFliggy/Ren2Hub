// Wire shapes of the top-up bonus APIs; see docs/billing/TOPUP_BONUS.md.
// Rates are decimal strings and thresholds integer quota strings, so neither
// passes through a JSON number.

import type { Quota } from '@/utils/quota'

export const TOPUP_BONUS_MODES = ['off', 'shadow', 'on'] as const
export type TopupBonusMode = (typeof TOPUP_BONUS_MODES)[number]

export const TOPUP_BONUS_WINDOWS = ['day', 'week', 'month'] as const
export type TopupBonusWindow = (typeof TOPUP_BONUS_WINDOWS)[number]

export interface TopupBonusTier {
  threshold_quota: string
  rate: string
}

export interface TopupBonusTable {
  mode: TopupBonusMode
  tiers: TopupBonusTier[]
}

export interface TopupBonusConfig {
  schema_version: 1
  timezone: string
  cap_rate: string
  fixed_rate: string
  amount: TopupBonusTable
  site: TopupBonusTable
  personal: Record<TopupBonusWindow, TopupBonusTable>
  excluded_user_ids: number[]
}

export interface TopupBonusSettings {
  version: number
  created_at: number
  created_by: number
  config: TopupBonusConfig
}

export interface TopupBonusFailureStat {
  count: number
  /** Unix seconds; 0 when the failure never happened in this process. */
  last_at: number
}

export const TOPUP_BONUS_HEALTH_KEYS = [
  'hourly_write_failures',
  'window_query_failures',
  'cleanup_failures',
] as const
export type TopupBonusHealthKey = (typeof TOPUP_BONUS_HEALTH_KEYS)[number]

export interface TopupBonusStatus {
  business_date: string
  cutoff_utc: number
  timezone: string
  config_version: number
  cached_users: number
  site_month_quota: string
  site_tier_rate: string
  earliest_hour_utc: number | null
  health: Record<TopupBonusHealthKey, TopupBonusFailureStat>
}

export const TOPUP_BONUS_SOURCES = [
  'topup',
  'redemption',
  'admin_quota',
] as const
export type TopupBonusSource = (typeof TOPUP_BONUS_SOURCES)[number]

export interface TopupBonusEstimateSource {
  source: TopupBonusSource
  records: number
  principal_quota: Quota
  bonus_quota: Quota
}

/** A replay of recent credits of every bonus source, not a bill. */
export interface TopupBonusEstimate {
  from_date: string
  to_date: string
  days: number
  records: number
  unrated_records: number
  principal_quota: Quota
  bonus_quota: Quota
  bonus_share: string
  capped_principal_share: string
  sources: TopupBonusEstimateSource[]
  earliest_hour_utc: number | null
  history_covers_start: boolean
}

export const TOPUP_BONUS_SELF_STATUSES = ['inactive', 'ok'] as const
export type TopupBonusSelfStatus = (typeof TOPUP_BONUS_SELF_STATUSES)[number]

export interface TopupBonusSelfWindow {
  window: TopupBonusWindow
  quota: string
  rate: string
  tiers: TopupBonusTier[]
}

/**
 * The bonus rate of the caller's next credit before its amount-dependent
 * tier, which is listed in amount_tiers. Never carries the site total or
 * shadow parts.
 */
export interface TopupBonusSelf {
  status: TopupBonusSelfStatus
  fixed_rate: string
  site_rate: string
  personal_rate: string
  raw_rate: string
  applied_rate: string
  cap_rate: string
  cap_applied: boolean
  winner_window?: TopupBonusWindow
  business_date?: string
  amount_tiers: TopupBonusTier[]
  windows: TopupBonusSelfWindow[]
}

/**
 * service.TopUpBonusQuote.PublicView: the bonus of one credit of
 * principal_quota; expected_quota is principal plus bonus.
 */
export interface TopupBonusQuote {
  principal_quota: Quota
  bonus_quota: Quota
  expected_quota: Quota
  applied_rate: string
  amount_rate: string
  fixed_rate: string
  site_rate: string
  personal_rate: string
  cap_applied: boolean
  winner_window?: TopupBonusWindow
}
