import { api } from './client'
import { isDecimal } from '@/utils/decimal'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredQuota,
  requiredString,
} from './contracts'

export interface BillingEvent {
  event_id: string
  request_id: string
  operation: string
  actor_id: number
  available_delta: string
  reserved_delta: string
  used_delta: string
  token_available_delta: string
  token_reserved_delta: string
  token_used_delta: string
  available_after: string
  reserved_after: string
  used_after: string
  reason: string
  created_at: number
}

export interface BillingAdjustment {
  kind: 'channel_multiplier'
  multiplier: string
  base_usd: string
  amount_usd: string
  result_usd: string
}
function billingDecimal(
  value: unknown,
  endpoint: string,
  signed = false
): string {
  if (
    typeof value !== 'string' ||
    !isDecimal(signed && value.startsWith('-') ? value.slice(1) : value)
  )
    invalidResponse(endpoint)
  return value
}
export interface BillingCharge {
  base_usd: string
  adjustments: BillingAdjustment[]
  lines: Array<{
    quantity: string
    count: string
    unit: string
    unit_price: string
    subtotal_usd: string
  }>
  final_quota: string
  total_usd: string
  unrounded_quota: string
  rounding_delta: string
  calculation_version: string
  rounding_version: string
  trace?: {
    matched_tier: string
    image_count?: number
    request_rules?: Array<{
      cond: string
      multiplier: string
      matched: boolean
    }>
  }
}

export function parseBillingCharge(
  value: unknown,
  endpoint: string
): BillingCharge | null {
  if (value === null || value === undefined) return null
  if (!isRecord(value) || !Array.isArray(value.lines)) invalidResponse(endpoint)
  let trace: BillingCharge['trace']
  if (value.trace != null) {
    if (!isRecord(value.trace)) invalidResponse(endpoint)
    trace = { matched_tier: requiredString(value.trace.matched_tier, endpoint) }
    if (value.trace.image_count !== undefined) {
      const count = requiredStrictInteger(value.trace.image_count, endpoint)
      if (count < 1 || count > 128) invalidResponse(endpoint)
      trace.image_count = count
    }
    if (value.trace.request_rules !== undefined) {
      if (!Array.isArray(value.trace.request_rules)) invalidResponse(endpoint)
      trace.request_rules = value.trace.request_rules.map((rule) => {
        if (!isRecord(rule) || !isDecimal(rule.multiplier))
          invalidResponse(endpoint)
        return {
          cond: requiredString(rule.cond, endpoint),
          multiplier: rule.multiplier,
          matched: requiredBoolean(rule.matched, endpoint),
        }
      })
    }
  }
  if (
    value.calculation_version !== 'billing-v3' ||
    !Array.isArray(value.adjustments)
  )
    invalidResponse(endpoint)
  return {
    base_usd: billingDecimal(value.base_usd, endpoint),
    adjustments: value.adjustments.map((item) => {
      if (!isRecord(item) || item.kind !== 'channel_multiplier')
        invalidResponse(endpoint)
      return {
        kind: 'channel_multiplier',
        multiplier: billingDecimal(item.multiplier, endpoint),
        base_usd: billingDecimal(item.base_usd, endpoint),
        amount_usd: billingDecimal(item.amount_usd, endpoint, true),
        result_usd: billingDecimal(item.result_usd, endpoint),
      }
    }),
    lines: value.lines.map((line) => {
      if (!isRecord(line)) invalidResponse(endpoint)
      return {
        quantity: requiredString(line.quantity, endpoint),
        count: requiredString(line.count, endpoint),
        unit: requiredString(line.unit, endpoint),
        unit_price: requiredString(line.unit_price, endpoint),
        subtotal_usd: requiredString(line.subtotal_usd, endpoint),
      }
    }),
    final_quota: requiredQuota(value.final_quota, endpoint),
    total_usd: requiredString(value.total_usd, endpoint),
    unrounded_quota: requiredString(value.unrounded_quota, endpoint),
    rounding_delta: requiredString(value.rounding_delta, endpoint),
    calculation_version: requiredString(value.calculation_version, endpoint),
    rounding_version: requiredString(value.rounding_version, endpoint),
    ...(trace ? { trace } : {}),
  }
}

export interface BillingPage<T> {
  items: T[]
  total: number
}

export function parseBillingEvent(
  value: unknown,
  endpoint: string
): BillingEvent {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    event_id: requiredString(value.event_id, endpoint, false),
    request_id: requiredString(value.request_id, endpoint),
    operation: requiredString(value.operation, endpoint, false),
    actor_id: requiredStrictInteger(value.actor_id, endpoint),
    available_delta: requiredQuota(value.available_delta, endpoint),
    reserved_delta: requiredQuota(value.reserved_delta, endpoint),
    used_delta: requiredQuota(value.used_delta, endpoint),
    token_available_delta: requiredQuota(value.token_available_delta, endpoint),
    token_reserved_delta: requiredQuota(value.token_reserved_delta, endpoint),
    token_used_delta: requiredQuota(value.token_used_delta, endpoint),
    available_after: requiredQuota(value.available_after, endpoint),
    reserved_after: requiredQuota(value.reserved_after, endpoint),
    used_after: requiredQuota(value.used_after, endpoint),
    reason: requiredString(value.reason, endpoint),
    created_at: requiredStrictInteger(value.created_at, endpoint),
  }
}

function parseBillingPage<T>(
  value: unknown,
  endpoint: string,
  parse: (item: unknown, endpoint: string) => T
): BillingPage<T> {
  if (!isRecord(value) || !Array.isArray(value.items)) invalidResponse(endpoint)
  const total = requiredStrictInteger(value.total, endpoint)
  if (total < 0) invalidResponse(endpoint)
  return { items: value.items.map((item) => parse(item, endpoint)), total }
}

export const billingApi = {
  async events(
    offset: number,
    limit: number,
    signal?: AbortSignal,
    tokenId?: number
  ) {
    const endpoint = '/api/billing/self/events'
    return parseBillingPage(
      await api.get(
        endpoint,
        { offset, limit, ...(tokenId ? { token_id: tokenId } : {}) },
        { signal }
      ),
      endpoint,
      parseBillingEvent
    )
  },
}
