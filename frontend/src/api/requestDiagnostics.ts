import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parsePage,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import type { PageResult } from './types'

export interface RequestAttemptDiagnostic {
  sequence: number
  attempt_id: string
  channel_id?: number
  credential_id?: string
  success: boolean
  error_class?: string
  error_http_status?: number
  origin: string
  latency_ms: number
  upstream_latency_ms?: number
  queue_ms: number
  send_started_at: number
  first_output_at?: number
  finished_at: number
  ttft_ms?: number
  termination?: string
  acceptance: string
  retry_permission: string
  response_committed: boolean
}

export interface RequestDiagnostic {
  schema_version: 1
  request_id: string
  token_id: number
  trace_id?: string
  thread_id?: string
  model_name: string
  created_at: number
  wait_ms: number
  user_queue_ms: number
  final_error?: string
  attempts: RequestAttemptDiagnostic[]
}

export interface RequestDiagnosticFilter {
  token_id: number
  trace_id?: string
  thread_id?: string
  request_id?: string
}

export function parseRequestDiagnostic(
  value: unknown,
  admin = false
): RequestDiagnostic {
  const endpoint = '/api/log/request-diagnostics'
  if (
    !isRecord(value) ||
    value.schema_version !== 1 ||
    !Array.isArray(value.attempts)
  )
    invalidResponse(endpoint)
  const nonNegative = (item: unknown) => {
    const result = requiredStrictNumber(item, endpoint)
    if (result < 0) invalidResponse(endpoint)
    return result
  }
  const optionalText = (item: unknown) =>
    item == null ? undefined : requiredString(item, endpoint)
  return {
    schema_version: 1,
    request_id: requiredString(value.request_id, endpoint, false),
    token_id: requiredStrictInteger(value.token_id, endpoint),
    trace_id: optionalText(value.trace_id),
    thread_id: optionalText(value.thread_id),
    model_name: requiredString(value.model_name, endpoint),
    created_at: nonNegative(value.created_at),
    wait_ms: nonNegative(value.wait_ms),
    user_queue_ms: nonNegative(value.user_queue_ms),
    final_error: optionalText(value.final_error),
    attempts: value.attempts.map((attempt) => {
      if (!isRecord(attempt)) invalidResponse(endpoint)
      const item: RequestAttemptDiagnostic = {
        sequence: requiredStrictInteger(attempt.sequence, endpoint),
        attempt_id: requiredString(attempt.attempt_id, endpoint, false),
        success: requiredBoolean(attempt.success, endpoint),
        error_class: optionalText(attempt.error_class),
        error_http_status:
          attempt.error_http_status == null
            ? undefined
            : requiredStrictInteger(attempt.error_http_status, endpoint),
        origin: requiredString(attempt.origin, endpoint),
        latency_ms: nonNegative(attempt.latency_ms),
        upstream_latency_ms:
          attempt.upstream_latency_ms == null
            ? undefined
            : nonNegative(attempt.upstream_latency_ms),
        queue_ms: nonNegative(attempt.queue_ms),
        send_started_at: nonNegative(attempt.send_started_at),
        first_output_at:
          attempt.first_output_at == null
            ? undefined
            : nonNegative(attempt.first_output_at),
        finished_at: nonNegative(attempt.finished_at),
        ttft_ms:
          attempt.ttft_ms == null ? undefined : nonNegative(attempt.ttft_ms),
        termination: optionalText(attempt.termination),
        acceptance: requiredString(attempt.acceptance, endpoint),
        retry_permission: requiredString(attempt.retry_permission, endpoint),
        response_committed: requiredBoolean(
          attempt.response_committed,
          endpoint
        ),
      }
      if (admin) {
        if (attempt.channel_id != null)
          item.channel_id = requiredStrictInteger(attempt.channel_id, endpoint)
        if (attempt.credential_id != null)
          item.credential_id = requiredString(attempt.credential_id, endpoint)
      }
      return item
    }),
  }
}

export async function getRequestDiagnostic(
  requestId: string,
  admin: boolean,
  signal?: AbortSignal
): Promise<RequestDiagnostic> {
  const prefix = admin ? '/api/log' : '/api/log/self'
  return parseRequestDiagnostic(
    await api.get<unknown>(
      `${prefix}/requests/${encodeURIComponent(requestId)}/diagnostics`,
      undefined,
      { signal }
    ),
    admin
  )
}

export async function listRequestDiagnostics(
  filters: RequestDiagnosticFilter,
  page: number,
  pageSize: number,
  admin: boolean,
  signal?: AbortSignal
): Promise<PageResult<RequestDiagnostic>> {
  const endpoint = admin
    ? '/api/log/request-diagnostics'
    : '/api/log/self/request-diagnostics'
  const value = await api.get<unknown>(
    endpoint,
    { ...filters, p: page, page_size: pageSize },
    { signal }
  )
  return parsePage(value, endpoint, (entry) =>
    parseRequestDiagnostic(entry, admin)
  )
}
