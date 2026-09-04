import type {
  LoginSession,
  LoginSessionBulkRevokeResult,
  LoginSessionRevokeResult,
} from '@/types/auth'
import { ApiError, type PageResult } from './types'

export function invalidResponse(endpoint: string): never {
  throw new ApiError(`Invalid API response: ${endpoint}`, {
    status: 502,
    code: 'INVALID_RESPONSE',
  })
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

export function requiredString(
  value: unknown,
  endpoint: string,
  allowEmpty = true
): string {
  if (typeof value !== 'string' || (!allowEmpty && value.length === 0)) {
    invalidResponse(endpoint)
  }
  return value
}

export function requiredNumber(value: unknown, endpoint: string): number {
  const parsed = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(parsed)) invalidResponse(endpoint)
  return parsed
}

export function requiredInteger(value: unknown, endpoint: string): number {
  const parsed = requiredNumber(value, endpoint)
  if (!Number.isSafeInteger(parsed)) invalidResponse(endpoint)
  return parsed
}

export function requiredStrictNumber(value: unknown, endpoint: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    invalidResponse(endpoint)
  }
  return value
}

export function requiredStrictInteger(
  value: unknown,
  endpoint: string
): number {
  const parsed = requiredStrictNumber(value, endpoint)
  if (!Number.isSafeInteger(parsed)) invalidResponse(endpoint)
  return parsed
}

export function requiredBoolean(value: unknown, endpoint: string): boolean {
  if (typeof value !== 'boolean') invalidResponse(endpoint)
  return value
}

export function parsePage<T>(
  value: unknown,
  endpoint: string,
  parseItem: (item: unknown, endpoint: string) => T
): PageResult<T> {
  if (!isRecord(value) || !Array.isArray(value.items)) invalidResponse(endpoint)
  const page = requiredInteger(value.page, endpoint)
  const pageSize = requiredInteger(value.page_size ?? value.pageSize, endpoint)
  const total = requiredInteger(value.total, endpoint)
  if (page < 1 || pageSize < 1 || total < 0) invalidResponse(endpoint)
  return {
    items: value.items.map((item) => parseItem(item, endpoint)),
    total,
    page,
    pageSize,
  }
}

export function parseStringArray(value: unknown, endpoint: string): string[] {
  if (!Array.isArray(value) || value.some((item) => typeof item !== 'string')) {
    invalidResponse(endpoint)
  }
  return [...value]
}

export function parseAccessToken(
  value: unknown,
  endpoint = '/api/user/token'
): string {
  return requiredString(value, endpoint, false)
}

export function parseLoginSession(
  value: unknown,
  endpoint = '/api/user/sessions'
): LoginSession {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    sid: requiredString(value.sid, endpoint, false),
    current: requiredBoolean(value.current, endpoint),
    login_method: requiredString(value.login_method, endpoint),
    ip: requiredString(value.ip, endpoint),
    user_agent: requiredString(value.user_agent, endpoint),
    created_at: requiredStrictNumber(value.created_at, endpoint),
    last_active_at: requiredStrictNumber(value.last_active_at, endpoint),
    expires_at: requiredStrictNumber(value.expires_at, endpoint),
  }
}

export function parseLoginSessions(
  value: unknown,
  endpoint = '/api/user/sessions'
): LoginSession[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => parseLoginSession(item, endpoint))
}

export function parseLoginSessionRevokeResult(
  value: unknown,
  endpoint = '/api/user/sessions/:sid'
): LoginSessionRevokeResult {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    revoked_sid: requiredString(value.revoked_sid, endpoint, false),
    current: requiredBoolean(value.current, endpoint),
  }
}

export function parseLoginSessionBulkRevokeResult(
  value: unknown,
  endpoint = '/api/user/sessions/revoke-others'
): LoginSessionBulkRevokeResult {
  if (!isRecord(value)) invalidResponse(endpoint)
  const revokedCount = requiredStrictInteger(value.revoked_count, endpoint)
  if (revokedCount < 0) invalidResponse(endpoint)
  return { revoked_count: revokedCount }
}
