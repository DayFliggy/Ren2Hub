import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredQuota,
  requiredString,
  requiredBoolean,
} from './contracts'
import { isDecimal } from '@/utils/decimal'

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

export interface ChargeLine {
  quantity: string
  count: string
  unit: string
  unit_price: string
  subtotal_usd: string
}

export interface ChargeAdjustment {
  kind: string
  multiplier: string
  base_usd: string
  amount_usd: string
  result_usd: string
}

export interface ChargeTrace {
  matched_tier?: string
  image_count?: number
  request_rules?: Array<{
    cond: string
    multiplier: string
    matched: boolean
  }>
}

export interface BillingCharge {
  lines: ChargeLine[]
  base_usd: string
  adjustments: ChargeAdjustment[]
  total_usd: string
  unrounded_quota: string
  final_quota: string
  rounding_delta: string
  calculation_version: string
  rounding_version: string
  trace?: ChargeTrace
}

function requiredPositiveDecimal(value: unknown, endpoint: string): string {
  if (!isDecimal(value)) invalidResponse(endpoint)
  return value
}

function requiredSignedDecimal(value: unknown, endpoint: string): string {
  if (typeof value !== 'string') invalidResponse(endpoint)
  const normalized = value.startsWith('-') ? value.slice(1) : value
  if (!isDecimal(normalized)) invalidResponse(endpoint)
  return value
}

function requiredTraceMultiplier(value: unknown, endpoint: string): string {
  if (isDecimal(value)) return value
  // Expr trace metadata uses Go's FormatFloat('g'); it is not a money field.
  if (
    typeof value !== 'string' ||
    value.length > 128 ||
    !/^[1-9](?:\.\d+)?e[+-]\d{2,3}$/.test(value) ||
    !Number.isFinite(Number(value)) ||
    Number(value) <= 0
  )
    invalidResponse(endpoint)
  return value
}

const KNOWN_CHARGE_VERSIONS = [
  ['newapi-c71eefbc-v1', 'newapi-v1'],
  // The current violation-fee branch retains its explicit half-up policy.
  ['newapi-c71eefbc-v1', 'half-up-positive-minimum-v1'],
  ['billing-v3', 'half-up-positive-minimum-v1'],
] as const

export function parseBillingCharge(
  value: unknown,
  endpoint: string
): BillingCharge {
  if (!isRecord(value)) invalidResponse(endpoint)
  if (!Array.isArray(value.lines)) invalidResponse(endpoint)
  if (!Array.isArray(value.adjustments)) invalidResponse(endpoint)

  const lines: ChargeLine[] = value.lines.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    return {
      quantity: requiredString(item.quantity, endpoint, false),
      count: requiredPositiveDecimal(item.count, endpoint),
      unit: requiredString(item.unit, endpoint, false),
      unit_price: requiredPositiveDecimal(item.unit_price, endpoint),
      subtotal_usd: requiredPositiveDecimal(item.subtotal_usd, endpoint),
    }
  })

  const adjustments: ChargeAdjustment[] = value.adjustments.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    return {
      kind: requiredString(item.kind, endpoint, false),
      multiplier: requiredPositiveDecimal(item.multiplier, endpoint),
      base_usd: requiredPositiveDecimal(item.base_usd, endpoint),
      amount_usd: requiredSignedDecimal(item.amount_usd, endpoint),
      result_usd: requiredPositiveDecimal(item.result_usd, endpoint),
    }
  })

  let trace: ChargeTrace | undefined
  if (value.trace !== null && value.trace !== undefined) {
    if (!isRecord(value.trace)) invalidResponse(endpoint)
    trace = {}
    if (value.trace.matched_tier !== undefined) {
      trace.matched_tier = requiredString(value.trace.matched_tier, endpoint)
    }
    if (value.trace.image_count !== undefined) {
      const count = requiredStrictInteger(value.trace.image_count, endpoint)
      if (count < 1 || count > 128) invalidResponse(endpoint)
      trace.image_count = count
    }
    if (value.trace.request_rules !== undefined) {
      if (!Array.isArray(value.trace.request_rules)) invalidResponse(endpoint)
      trace.request_rules = value.trace.request_rules.map((rule) => {
        if (!isRecord(rule)) invalidResponse(endpoint)
        return {
          cond: requiredString(rule.cond, endpoint),
          multiplier: requiredTraceMultiplier(rule.multiplier, endpoint),
          matched: requiredBoolean(rule.matched, endpoint),
        }
      })
    }
  }

  const calculationVersion = requiredString(
    value.calculation_version,
    endpoint,
    false
  )
  const roundingVersion = requiredString(
    value.rounding_version,
    endpoint,
    false
  )
  if (
    !KNOWN_CHARGE_VERSIONS.some(
      ([calculation, rounding]) =>
        calculation === calculationVersion && rounding === roundingVersion
    )
  )
    invalidResponse(endpoint)

  const baseUsd = requiredPositiveDecimal(value.base_usd, endpoint)
  const totalUsd = requiredPositiveDecimal(value.total_usd, endpoint)
  const unroundedQuota = requiredPositiveDecimal(
    value.unrounded_quota,
    endpoint
  )
  const finalQuota = requiredQuota(value.final_quota, endpoint)
  const roundingDelta = requiredSignedDecimal(value.rounding_delta, endpoint)

  if (BigInt(finalQuota) < 0n) invalidResponse(endpoint)

  return {
    lines,
    base_usd: baseUsd,
    adjustments,
    total_usd: totalUsd,
    unrounded_quota: unroundedQuota,
    final_quota: finalQuota,
    rounding_delta: roundingDelta,
    calculation_version: calculationVersion,
    rounding_version: roundingVersion,
    trace,
  }
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
