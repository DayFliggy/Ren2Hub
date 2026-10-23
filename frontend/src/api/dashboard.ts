import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import type { PublicStatus } from './public'
import { dateInputValue, viewerTzOffset } from '@/utils/format'
import type { StatsPeriod } from '@/composables/useDashboardStats'
import type { TokenTrendPoint } from '@/composables/useDashboard'
import type { UsageDistributionPoint } from '@/composables/useUsageDistribution'

export interface UsageSummary extends StatsPeriod {
  token_trend: TokenTrendPoint[]
  distribution: UsageDistributionPoint[]
  meta: {
    start: number
    end: number
    tz_offset: number
    generated_at: number
    /** False means no data source (data export is off), not zero usage. */
    available: boolean
    source: 'quota_data'
    bucket_seconds: number
    /** quota_data is flushed on this interval, so the latest hour may lag. */
    flush_interval_minutes: number
    coverage: { latency: boolean; success_rate: boolean }
  }
}
const endpoint = '/api/data/self/summary'
const list = (value: unknown): Record<string, unknown>[] => {
  if (!Array.isArray(value) || value.some((row) => !isRecord(row)))
    invalidResponse(endpoint)
  return value as Record<string, unknown>[]
}
const number = (value: unknown) => requiredStrictNumber(value, endpoint)
const integer = (value: unknown) => requiredStrictInteger(value, endpoint)
function quota(value: unknown): string {
  if (typeof value === 'number' && Number.isSafeInteger(value))
    return String(value)
  if (typeof value === 'string' && /^-?(0|[1-9]\d*)$/.test(value)) {
    try {
      const parsed = BigInt(value)
      if (parsed >= -(1n << 63n) && parsed <= (1n << 63n) - 1n) return value
    } catch {
      // Fall through to the endpoint-specific invalid-response error.
    }
  }
  invalidResponse(endpoint)
}
const nullable = (value: unknown) => (value === null ? null : number(value))
// A dimension whose log source is off must stay unknown; a number would be fabricated.
function measured(value: unknown, covered: boolean) {
  if (!covered && value !== null) invalidResponse(endpoint)
  return nullable(value)
}
function successRate(value: unknown, covered: boolean) {
  const rate = measured(value, covered)
  if (rate !== null && (rate < 0 || rate > 100)) invalidResponse(endpoint)
  return rate
}
export function parseUsageSummary(value: unknown): UsageSummary {
  if (
    !isRecord(value) ||
    !isRecord(value.kpi) ||
    (value.comparison !== null && !isRecord(value.comparison)) ||
    !isRecord(value.meta) ||
    !isRecord(value.meta.coverage) ||
    value.meta.source !== 'quota_data'
  )
    invalidResponse(endpoint)
  const latency = requiredBoolean(value.meta.coverage.latency, endpoint)
  const success = requiredBoolean(value.meta.coverage.success_rate, endpoint)
  return {
    kpi: {
      totalTokens: integer(value.kpi.totalTokens),
      totalQuota: quota(value.kpi.totalQuota),
      totalRequests: integer(value.kpi.totalRequests),
      avgLatency: measured(value.kpi.avgLatency, latency),
      successRate: successRate(value.kpi.successRate, success),
    },
    comparison:
      value.comparison === null
        ? null
        : {
            quotaDelta: nullable(value.comparison.quotaDelta),
            requestsDelta: nullable(value.comparison.requestsDelta),
          },
    models: list(value.models).map((row) => ({
      model: requiredString(row.model, endpoint),
      tokens: integer(row.tokens),
      quota: quota(row.quota),
      requests: integer(row.requests),
      share: number(row.share),
      avgLatency: measured(row.avgLatency, latency),
    })),
    hourly: list(value.hourly).map((row) => ({
      ts: integer(row.ts),
      requests: integer(row.requests),
      tokens: integer(row.tokens),
      quota: quota(row.quota),
    })),
    flow: list(value.flow).map((row) => ({
      date: requiredString(row.date, endpoint),
      consume: quota(row.consume),
      requests: integer(row.requests),
    })),
    token_trend: list(value.token_trend).map((row) => ({
      date: requiredString(row.date, endpoint),
      total: integer(row.total),
      cache_read: integer(row.cache_read),
      cache_create: integer(row.cache_create),
    })),
    distribution: list(value.distribution).map((row) => ({
      date: requiredString(row.date, endpoint),
      requests: integer(row.requests),
      consume: quota(row.consume),
      tokens: integer(row.tokens),
    })),
    meta: {
      start: number(value.meta.start),
      end: number(value.meta.end),
      tz_offset: number(value.meta.tz_offset),
      generated_at: number(value.meta.generated_at),
      available: requiredBoolean(value.meta.available, endpoint),
      source: 'quota_data',
      bucket_seconds: integer(value.meta.bucket_seconds),
      flush_interval_minutes: integer(value.meta.flush_interval_minutes),
      coverage: { latency, success_rate: success },
    },
  }
}
/** A preset ending today, or inclusive local 'YYYY-MM-DD' dates. */
export type SummaryRange =
  | { range: 'today' | '7d' | '30d' }
  | { range: 'custom'; start: string; end: string }

const PRESET_DAYS = { today: 1, '7d': 7, '30d': 30 } as const
/** ParseDashboardRange's own cap; it applies even when the log limit is higher. */
export const SUMMARY_MAX_DAYS = 366

// UTC day numbers keep every day 24 hours, like the backend's fixed offset.
function dayNumber(key: string): number {
  const [year, month, day] = key.split('-').map(Number)
  return Date.UTC(year!, month! - 1, day!) / 86_400_000
}

/** The current calendar month up to today. */
export function currentMonthRange(today = dateInputValue()): SummaryRange {
  return { range: 'custom', start: `${today.slice(0, 8)}01`, end: today }
}

/** log_max_range_days from /api/status; the backend always sends it. */
export function logRangeLimit(status: PublicStatus): number {
  if (status.log_max_range_days === undefined) invalidResponse('/api/status')
  return status.log_max_range_days
}

/**
 * Mirrors GetDashboardSummary: the window spans at most 366 days, and its
 * days up to today, plus the whole prior window when comparing, must fit
 * log_max_range_days.
 */
export function summaryFits(
  request: SummaryRange,
  limit: number,
  comparison: boolean,
  today = dateInputValue()
): boolean {
  const now = dayNumber(today)
  const [first, last] =
    request.range === 'custom'
      ? [dayNumber(request.start), dayNumber(request.end)]
      : [now - PRESET_DAYS[request.range] + 1, now]
  const days = last - first + 1
  const logged = Math.min(last, now) - first + 1
  return days <= SUMMARY_MAX_DAYS && logged + (comparison ? days : 0) <= limit
}

/** include_comparison is always explicit; the backend default would compare. */
export async function getUsageSummary(
  request: SummaryRange,
  includeComparison: boolean,
  signal?: AbortSignal
) {
  return parseUsageSummary(
    await api.get(
      endpoint,
      {
        tz_offset: viewerTzOffset(),
        ...request,
        include_comparison: String(includeComparison),
      },
      { signal }
    )
  )
}
