import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredQuota,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import { viewerTzOffset } from '@/utils/format'
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
      totalQuota: requiredQuota(value.kpi.totalQuota, endpoint),
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
      quota: requiredQuota(row.quota, endpoint),
      requests: integer(row.requests),
      share: number(row.share),
      avgLatency: measured(row.avgLatency, latency),
    })),
    hourly: list(value.hourly).map((row) => ({
      ts: integer(row.ts),
      requests: integer(row.requests),
      tokens: integer(row.tokens),
      quota: requiredQuota(row.quota, endpoint),
    })),
    flow: list(value.flow).map((row) => ({
      date: requiredString(row.date, endpoint),
      consume: requiredQuota(row.consume, endpoint),
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
      consume: requiredQuota(row.consume, endpoint),
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
export async function getUsageSummary(
  params: Record<string, string> = { range: '30d' },
  signal?: AbortSignal
) {
  return parseUsageSummary(
    await api.get(
      endpoint,
      { tz_offset: viewerTzOffset(), ...params },
      { signal }
    )
  )
}
