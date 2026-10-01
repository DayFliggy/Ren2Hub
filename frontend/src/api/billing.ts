import { api } from './client'
import {
  invalidResponse,
  isRecord,
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
