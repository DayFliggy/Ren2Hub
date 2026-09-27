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

export interface BillingReservation {
  request_id: string
  user_id: number
  token_id: number
  state: string
  funding_amount: string
  token_amount: string
  version: number
  created_at: number
  updated_at: number
  last_error: string
  manual_review_required: boolean
}

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

export interface BillingCharge {
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
  return {
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

export interface BillingMetrics {
  pending: number
  reconciliations: number
  reserved_quota: string
  oldest_wait_seconds: number
  idempotency_conflicts: number
  recovery_failures: number
  debt_events: number
}

export interface BillingDetail {
  recovery: BillingReservation
  quote: unknown
  attempts: unknown[]
  usage: unknown
  charge: BillingCharge | null
  chargeUnavailable: boolean
  events: BillingEvent[]
}

export interface BillingPage<T> {
  items: T[]
  total: number
}
export interface BillingResolution {
  operation_id: string
  expected_version: number
  action: 'settle' | 'release'
  quota: string
  reason: string
}

export function parseBillingReservation(
  value: unknown,
  endpoint: string
): BillingReservation {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    request_id: requiredString(value.request_id, endpoint, false),
    user_id: requiredStrictInteger(value.user_id, endpoint),
    token_id: requiredStrictInteger(value.token_id, endpoint),
    state: requiredString(value.state, endpoint, false),
    funding_amount: requiredQuota(value.funding_amount, endpoint),
    token_amount: requiredQuota(value.token_amount, endpoint),
    version: requiredStrictInteger(value.version, endpoint),
    created_at: requiredStrictInteger(value.created_at, endpoint),
    updated_at: requiredStrictInteger(value.updated_at, endpoint),
    last_error: requiredString(value.last_error, endpoint),
    manual_review_required: requiredBoolean(
      value.manual_review_required,
      endpoint
    ),
  }
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

const reconciliationPath = '/api/billing/reconciliations'

export const billingApi = {
  async metrics(signal?: AbortSignal): Promise<BillingMetrics> {
    const endpoint = '/api/billing/metrics'
    const value = await api.get(endpoint, undefined, { signal })
    if (!isRecord(value)) invalidResponse(endpoint)
    const metrics = {
      pending: requiredStrictInteger(value.pending, endpoint),
      reconciliations: requiredStrictInteger(value.reconciliations, endpoint),
      reserved_quota: requiredQuota(value.reserved_quota, endpoint),
      oldest_wait_seconds: requiredStrictInteger(
        value.oldest_wait_seconds,
        endpoint
      ),
      idempotency_conflicts: requiredStrictInteger(
        value.idempotency_conflicts,
        endpoint
      ),
      recovery_failures: requiredStrictInteger(
        value.recovery_failures,
        endpoint
      ),
      debt_events: requiredStrictInteger(value.debt_events, endpoint),
    }
    if (Object.values(metrics).some((value) => BigInt(value) < 0n))
      invalidResponse(endpoint)
    return metrics
  },
  async reservations(
    offset: number,
    limit: number,
    signal?: AbortSignal,
    tokenId?: number
  ) {
    const endpoint = '/api/billing/self/reservations'
    return parseBillingPage(
      await api.get(
        endpoint,
        { offset, limit, ...(tokenId ? { token_id: tokenId } : {}) },
        { signal }
      ),
      endpoint,
      parseBillingReservation
    )
  },
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
  async reconciliations(
    offset: number,
    limit: number,
    status: string,
    signal?: AbortSignal
  ) {
    return parseBillingPage(
      await api.get(
        reconciliationPath,
        { offset, limit, ...(status ? { status } : {}) },
        { signal }
      ),
      reconciliationPath,
      parseBillingReservation
    )
  },
  async detail(id: string, signal?: AbortSignal): Promise<BillingDetail> {
    const endpoint = `${reconciliationPath}/${encodeURIComponent(id)}`
    const value = await api.get(endpoint, undefined, { signal })
    if (
      !isRecord(value) ||
      !Array.isArray(value.events) ||
      !Array.isArray(value.attempts)
    )
      invalidResponse(endpoint)
    return {
      recovery: parseBillingReservation(value.recovery, endpoint),
      quote: value.quote,
      attempts: value.attempts,
      usage: value.usage,
      charge:
        isRecord(value.charge) && value.charge.unavailable === true
          ? null
          : parseBillingCharge(value.charge, endpoint),
      chargeUnavailable:
        isRecord(value.charge) && value.charge.unavailable === true,
      events: value.events.map((event) => parseBillingEvent(event, endpoint)),
    }
  },
  retry(id: string, operationId: string, expectedVersion: number) {
    return api.post(`${reconciliationPath}/${encodeURIComponent(id)}/retry`, {
      operation_id: operationId,
      expected_version: expectedVersion,
    })
  },
  resolve(id: string, resolution: BillingResolution) {
    return api.post(
      `${reconciliationPath}/${encodeURIComponent(id)}/resolve`,
      resolution
    )
  },
}
