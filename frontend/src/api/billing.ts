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

export interface BillingMetrics {
  operational?: BillingOperationalMetrics
  price_versions: number
  quote_attempts: number
  prices_by_origin: Array<{ scope_kind: string; origin: string; count: number }>
  pending: number
  reconciliations: number
  reserved_quota: string
  oldest_wait_seconds: number
  idempotency_conflicts: number
  recovery_failures: number
  debt_events: number
}
export interface BillingOperationalMetrics {
  counter_scope: 'instance'
  started_at: number
  source_refresh_failures_total: Record<string, number>
  candidate_rejections_total: Record<string, number>
  missing_price_total: number
  unknown_usage_total: number
  calculation_total: number
  calculation_failures_total: number
  replay_mismatches_total: number
}
function parseOperationalMetrics(
  value: unknown,
  endpoint: string
): BillingOperationalMetrics {
  if (!isRecord(value) || value.counter_scope !== 'instance')
    invalidResponse(endpoint)
  const count = (raw: unknown) => {
    const parsed = requiredStrictInteger(raw, endpoint)
    if (parsed < 0) invalidResponse(endpoint)
    return parsed
  }
  const sourceCounts = (raw: unknown) => {
    if (
      !isRecord(raw) ||
      Object.keys(raw).some(
        (key) => !['sub2api', 'axonhub', 'other'].includes(key)
      )
    )
      invalidResponse(endpoint)
    return Object.fromEntries(
      Object.entries(raw).map(([key, amount]) => [key, count(amount)])
    )
  }
  return {
    counter_scope: 'instance',
    started_at: count(value.started_at),
    source_refresh_failures_total: sourceCounts(
      value.source_refresh_failures_total
    ),
    candidate_rejections_total: sourceCounts(value.candidate_rejections_total),
    missing_price_total: count(value.missing_price_total),
    unknown_usage_total: count(value.unknown_usage_total),
    calculation_total: count(value.calculation_total),
    calculation_failures_total: count(value.calculation_failures_total),
    replay_mismatches_total: count(value.replay_mismatches_total),
  }
}

export interface BillingDetail {
  recovery: BillingReservation
  quote: unknown
  attempts: unknown[]
  usage: unknown
  charge: BillingCharge | null
  chargeUnavailable: boolean
  audit: BillingAudit
  upstream_cost: BillingCostSummary
  events: BillingEvent[]
}

export interface BillingAuditAttempt {
  id: string
  channel_id: number
  request_fingerprint: string
  price_scope: string
  price_version_id: number
  price_version_hash: string
  quote_hash: string
  origin: string
  source_hash: string
}
export interface BillingAudit {
  status: 'unconfirmed' | 'verified' | 'mismatch' | 'invalid_evidence'
  reason?: string
  attempts: BillingAuditAttempt[]
  quote_snapshot_hash?: string
  usage_snapshot_hash?: string
  charge_snapshot_hash?: string
  replayed_charge: BillingCharge | null
  quoted_total_usd: string | null
  customer_charge_usd: string
  customer_charge_quota: string
  customer_charge_confirmed: boolean
  charge_difference_usd: string | null
  charge_difference_quota: string | null
  upstream_cost_usd: string | null
  profit_usd: string | null
  cost_status: 'unknown' | 'confirmed'
}
export interface BillingCostCommand {
  event_id: string
  attempt_id: string
  amount_usd: string
  source: 'invoice' | 'provider_receipt'
  reference: string
}
export interface BillingUpstreamCost extends BillingCostCommand {
  id: number
  request_id: string
  actor_id: number
  created_at: number
}
export interface BillingCostSummary {
  attempt_count: number
  confirmed_attempt_count: number
  complete: boolean
  upstream_cost_usd: string | null
  records: BillingUpstreamCost[]
}
function parseBillingCost(
  value: unknown,
  endpoint: string
): BillingUpstreamCost {
  if (
    !isRecord(value) ||
    !['invoice', 'provider_receipt'].includes(String(value.source))
  )
    invalidResponse(endpoint)
  return {
    id: requiredStrictInteger(value.id, endpoint),
    request_id: requiredString(value.request_id, endpoint),
    actor_id: requiredStrictInteger(value.actor_id, endpoint),
    created_at: requiredStrictInteger(value.created_at, endpoint),
    event_id: requiredString(value.event_id, endpoint),
    attempt_id: requiredString(value.attempt_id, endpoint),
    amount_usd: billingDecimal(value.amount_usd, endpoint),
    source: value.source as BillingCostCommand['source'],
    reference: requiredString(value.reference, endpoint),
  }
}
export function parseBillingCostSummary(
  value: unknown,
  endpoint: string
): BillingCostSummary {
  if (!isRecord(value) || !Array.isArray(value.records))
    invalidResponse(endpoint)
  return {
    attempt_count: requiredStrictInteger(value.attempt_count, endpoint),
    confirmed_attempt_count: requiredStrictInteger(
      value.confirmed_attempt_count,
      endpoint
    ),
    complete: requiredBoolean(value.complete, endpoint),
    upstream_cost_usd:
      value.upstream_cost_usd === null
        ? null
        : billingDecimal(value.upstream_cost_usd, endpoint),
    records: value.records.map((row) => parseBillingCost(row, endpoint)),
  }
}
export function parseBillingAudit(
  value: unknown,
  endpoint: string
): BillingAudit {
  if (
    !isRecord(value) ||
    !Array.isArray(value.attempts) ||
    !['unconfirmed', 'verified', 'mismatch', 'invalid_evidence'].includes(
      String(value.status)
    ) ||
    !['unknown', 'confirmed'].includes(String(value.cost_status))
  )
    invalidResponse(endpoint)
  const result: BillingAudit = {
    status: value.status as BillingAudit['status'],
    attempts: value.attempts.map((attempt) => {
      if (!isRecord(attempt)) invalidResponse(endpoint)
      return {
        id: requiredString(attempt.id, endpoint),
        channel_id: requiredStrictInteger(attempt.channel_id, endpoint),
        request_fingerprint: requiredString(
          attempt.request_fingerprint,
          endpoint
        ),
        price_scope: requiredString(attempt.price_scope, endpoint),
        price_version_id: requiredStrictInteger(
          attempt.price_version_id,
          endpoint
        ),
        price_version_hash: requiredString(
          attempt.price_version_hash,
          endpoint
        ),
        quote_hash: requiredString(attempt.quote_hash, endpoint),
        origin: requiredString(attempt.origin, endpoint),
        source_hash: requiredString(attempt.source_hash, endpoint),
      }
    }),
    replayed_charge: parseBillingCharge(value.replayed_charge, endpoint),
    quoted_total_usd:
      value.quoted_total_usd === null
        ? null
        : billingDecimal(value.quoted_total_usd, endpoint),
    customer_charge_usd: billingDecimal(value.customer_charge_usd, endpoint),
    customer_charge_quota: requiredQuota(value.customer_charge_quota, endpoint),
    customer_charge_confirmed: requiredBoolean(
      value.customer_charge_confirmed,
      endpoint
    ),
    charge_difference_usd:
      value.charge_difference_usd === null
        ? null
        : billingDecimal(value.charge_difference_usd, endpoint, true),
    charge_difference_quota:
      value.charge_difference_quota === null
        ? null
        : requiredQuota(value.charge_difference_quota, endpoint),
    upstream_cost_usd:
      value.upstream_cost_usd === null
        ? null
        : billingDecimal(value.upstream_cost_usd, endpoint),
    profit_usd:
      value.profit_usd === null
        ? null
        : billingDecimal(value.profit_usd, endpoint, true),
    cost_status: value.cost_status as BillingAudit['cost_status'],
  }
  for (const key of [
    'reason',
    'quote_snapshot_hash',
    'usage_snapshot_hash',
    'charge_snapshot_hash',
  ] as const) {
    if (value[key] !== undefined)
      result[key] = requiredString(value[key], endpoint)
  }
  if (
    result.profit_usd !== null &&
    (!result.customer_charge_confirmed || result.cost_status !== 'confirmed')
  )
    invalidResponse(endpoint)
  return result
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
    if (!Array.isArray(value.prices_by_origin)) invalidResponse(endpoint)
    const metrics = {
      price_versions: requiredStrictInteger(value.price_versions, endpoint),
      quote_attempts: requiredStrictInteger(value.quote_attempts, endpoint),
      prices_by_origin: value.prices_by_origin.map((row) => {
        if (!isRecord(row)) invalidResponse(endpoint)
        return {
          scope_kind: requiredString(row.scope_kind, endpoint),
          origin: requiredString(row.origin, endpoint),
          count: requiredStrictInteger(row.count, endpoint),
        }
      }),
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
    if (
      Object.entries(metrics).some(
        ([key, value]) =>
          key !== 'prices_by_origin' && BigInt(value as number | string) < 0n
      ) ||
      metrics.prices_by_origin.some((row) => row.count < 0)
    )
      invalidResponse(endpoint)
    return {
      ...metrics,
      ...(value.operational === undefined
        ? {}
        : {
            operational: parseOperationalMetrics(value.operational, endpoint),
          }),
    }
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
      audit: parseBillingAudit(value.audit, endpoint),
      upstream_cost: parseBillingCostSummary(value.upstream_cost, endpoint),
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
  async recordCost(
    id: string,
    command: BillingCostCommand
  ): Promise<BillingUpstreamCost> {
    const endpoint = `${reconciliationPath}/${encodeURIComponent(id)}/cost`
    return parseBillingCost(await api.post(endpoint, command), endpoint)
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
