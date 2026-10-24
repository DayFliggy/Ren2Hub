import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
} from './contracts'

export interface RequestLimits {
  enabled: boolean
  window_minutes: number
  total_requests: number
  successful_requests: number
}

// Signed values are intentional: the native limiter disables ceilings <= 0.
// Keep invalid settings visible instead of turning them into a fictitious RPM.
export function parseRequestLimits(value: unknown): RequestLimits {
  const endpoint = '/api/user/self'
  if (!isRecord(value) || !isRecord(value.request_limits))
    invalidResponse(endpoint)
  const limits = value.request_limits
  return {
    enabled: requiredBoolean(limits.enabled, endpoint),
    window_minutes: requiredStrictInteger(limits.window_minutes, endpoint),
    total_requests: requiredStrictInteger(limits.total_requests, endpoint),
    successful_requests: requiredStrictInteger(
      limits.successful_requests,
      endpoint
    ),
  }
}

export async function getRequestLimits(
  signal?: AbortSignal
): Promise<RequestLimits> {
  return parseRequestLimits(
    await api.get('/api/user/self', undefined, { signal })
  )
}
