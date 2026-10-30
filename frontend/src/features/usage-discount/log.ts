import { isRecord } from '@/api/contracts'
import { RATE_PATTERN } from '@/api/usageDiscount'
import {
  USAGE_DISCOUNT_SELF_STATUSES,
  USAGE_DISCOUNT_WINDOWS,
  type UsageDiscountSelfStatus,
  type UsageDiscountWindow,
} from '@/types/usageDiscount'
import { isDecimal } from '@/utils/decimal'

// Log evidence written by service/usage_discount_log.go. Users see the
// public other.usage_discount only when a component can change the price;
// administrators also get other.admin_info.usage_discount. A summary appears
// only for a well-formed object; the raw evidence is always shown beside it.

export const LOG_COMPONENTS = ['fixed', 'site', 'day', 'week', 'month'] as const
export type LogComponent = (typeof LOG_COMPONENTS)[number]

export interface LogUsageDiscount {
  status: UsageDiscountSelfStatus
  base_channel_ratio: string
  factor: string
  applied_rate: string
  rates: Record<LogComponent, string>
  winner_window?: UsageDiscountWindow
  cap_applied: boolean
}

export interface LogUsageDiscountAdmin {
  status: UsageDiscountSelfStatus
  raw_rate: string
  cap_rate: string
  config_version: number
  /** Would-be rates of shadow components, keyed site/day/week/month. */
  shadow_rates: Record<string, string>
}

function isRate(value: unknown): value is string {
  return typeof value === 'string' && RATE_PATTERN.test(value)
}

function isStatus(value: unknown): value is UsageDiscountSelfStatus {
  return (USAGE_DISCOUNT_SELF_STATUSES as readonly unknown[]).includes(value)
}

export function logUsageDiscount(
  evidence: Record<string, unknown>
): LogUsageDiscount | null {
  const value = evidence.usage_discount
  if (!isRecord(value) || !isStatus(value.status)) return null
  const rates = LOG_COMPONENTS.map((part) => value[`${part}_rate`])
  if (
    !isDecimal(value.base_channel_ratio) ||
    !isRate(value.factor) ||
    !isRate(value.applied_rate) ||
    !rates.every(isRate) ||
    typeof value.cap_applied !== 'boolean'
  )
    return null
  const winner = value.winner_window
  if (
    winner !== undefined &&
    !(USAGE_DISCOUNT_WINDOWS as readonly unknown[]).includes(winner)
  )
    return null
  return {
    status: value.status,
    base_channel_ratio: value.base_channel_ratio,
    factor: value.factor,
    applied_rate: value.applied_rate,
    rates: Object.fromEntries(
      LOG_COMPONENTS.map((part, index) => [part, rates[index]])
    ) as Record<LogComponent, string>,
    ...(winner === undefined
      ? {}
      : { winner_window: winner as UsageDiscountWindow }),
    cap_applied: value.cap_applied,
  }
}

export function logUsageDiscountAdmin(
  evidence: Record<string, unknown>
): LogUsageDiscountAdmin | null {
  const admin = isRecord(evidence.admin_info) ? evidence.admin_info : {}
  const value = admin.usage_discount
  if (!isRecord(value) || !isStatus(value.status)) return null
  const shadow = value.shadow_rates ?? {}
  if (
    !isRate(value.raw_rate) ||
    !isRate(value.cap_rate) ||
    !Number.isSafeInteger(value.config_version) ||
    !isRecord(shadow) ||
    !Object.values(shadow).every(isRate)
  )
    return null
  return {
    status: value.status,
    raw_rate: value.raw_rate,
    cap_rate: value.cap_rate,
    config_version: value.config_version as number,
    shadow_rates: shadow as Record<string, string>,
  }
}
