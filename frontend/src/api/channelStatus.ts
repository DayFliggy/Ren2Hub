import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import type { AdminChannel, AdminChannelPage } from '@/types/console'

export function parseChannelStatusDetails(value: unknown): AdminChannel {
  const endpoint = '/api/channel/'
  if (!isRecord(value)) invalidResponse(endpoint)
  const status = requiredStrictInteger(value.status, endpoint)
  if (status !== 1 && status !== 2 && status !== 3) invalidResponse(endpoint)
  let info: Record<string, unknown> = {}
  if (value.other_info) {
    try {
      const parsed: unknown = JSON.parse(
        requiredString(value.other_info, endpoint)
      )
      if (!isRecord(parsed)) invalidResponse(endpoint)
      info = parsed
    } catch {
      invalidResponse(endpoint)
    }
  }
  const time =
    info.status_time === undefined
      ? 0
      : requiredStrictInteger(info.status_time, endpoint)
  if (time < 0 || time > 253402300799) invalidResponse(endpoint)
  if (!isRecord(value.lab)) invalidResponse(endpoint)
  const lab = {
    id: requiredString(value.lab.id, endpoint),
    name: requiredString(value.lab.name, endpoint),
    source: requiredString(value.lab.source, endpoint),
  }
  return {
    ...value,
    lab,
    supplier: lab.name,
    lab_group_slug: lab.id,
    lab_group_name: lab.name,
    in_flight: requiredStrictInteger(value.in_flight, endpoint),
    id: requiredStrictInteger(value.id, endpoint),
    name: requiredString(value.name, endpoint),
    type: requiredStrictInteger(value.type, endpoint),
    channel_ratio: requiredString(value.channel_ratio, endpoint),
    max_concurrency: requiredStrictInteger(value.max_concurrency, endpoint),
    status,
    status_reason:
      info.status_reason === undefined
        ? ''
        : requiredString(info.status_reason, endpoint),
    status_time: time,
    priority: requiredStrictInteger(value.priority ?? 0, endpoint),
    weight: requiredStrictInteger(value.weight ?? 0, endpoint),
    used_quota: requiredStrictInteger(value.used_quota, endpoint),
    balance: requiredStrictNumber(value.balance, endpoint),
    response_time: requiredStrictInteger(value.response_time, endpoint),
    test_time: requiredStrictInteger(value.test_time, endpoint),
    base_url: requiredString(value.base_url ?? '', endpoint),
    models: requiredString(value.models, endpoint),
    model_mapping: requiredString(value.model_mapping ?? '', endpoint),
  }
}

export function parseChannelPage(value: unknown): AdminChannelPage {
  const endpoint = '/api/channel/'
  if (
    !isRecord(value) ||
    !Array.isArray(value.items) ||
    !isRecord(value.type_counts)
  )
    invalidResponse(endpoint)
  const total = requiredStrictInteger(value.total, endpoint)
  if (total < 0) invalidResponse(endpoint)
  const counts: Record<string, number> = {}
  for (const [type, count] of Object.entries(value.type_counts)) {
    counts[type] = requiredStrictInteger(count, endpoint)
    if (counts[type] < 0) invalidResponse(endpoint)
  }
  return {
    items: value.items.map(parseChannelStatusDetails),
    total,
    type_counts: counts,
  }
}

/**
 * Native test/balance endpoints place their results (time, balance,
 * raw_response) beside success, not in data, so callers read the envelope.
 */
export async function channelOperation(
  endpoint: string,
  params?: Record<string, unknown>,
  signal?: AbortSignal
): Promise<Record<string, unknown>> {
  const envelope = await api.getEnvelope<unknown>(endpoint, params, { signal })
  return envelope as Record<string, unknown>
}
