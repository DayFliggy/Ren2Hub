import { getLocale } from '@/i18n'
import { serverResponseMessage } from '@/i18n/serverMessages'
import { httpTransport } from './httpTransport'
import { ApiError } from './types'
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
  return {
    ...value,
    id: requiredStrictInteger(value.id, endpoint),
    name: requiredString(value.name, endpoint),
    type: requiredStrictInteger(value.type, endpoint),
    group: requiredString(value.group, endpoint),
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

/** Native test/balance endpoints place their results beside success, not in data. */
export async function channelOperation(
  endpoint: string,
  params?: Record<string, unknown>,
  signal?: AbortSignal
): Promise<Record<string, unknown>> {
  const response: unknown = await httpTransport.request('GET', endpoint, {
    params,
    signal,
  })
  if (!isRecord(response) || typeof response.success !== 'boolean')
    invalidResponse(endpoint)
  if (!response.success)
    throw new ApiError(
      serverResponseMessage(getLocale(), response) || 'Request failed',
      {
        business: true,
        code:
          typeof response.error_code === 'string'
            ? response.error_code
            : undefined,
      }
    )
  return response
}
