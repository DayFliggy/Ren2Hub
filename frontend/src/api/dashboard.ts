import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredQuota,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
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
    available: boolean
    source: 'consume_logs'
    cache_coverage: {
      records: number
      read_records: number
      write_records: number
      invalid_records: number
    }
    coverage: {
      consume_logs: boolean
      cache_usage: boolean
      latency: boolean
      success_rate: false
    }
  }
}
const endpoint = '/api/data/self/summary'
const list = (value: unknown): Record<string, unknown>[] => {
  if (!Array.isArray(value) || value.some((row) => !isRecord(row)))
    invalidResponse(endpoint)
  return value as Record<string, unknown>[]
}
const number = (value: unknown) => requiredStrictNumber(value, endpoint)
const nullable = (value: unknown) => (value === null ? null : number(value))
export function parseUsageSummary(value: unknown): UsageSummary {
  if (
    !isRecord(value) ||
    !isRecord(value.kpi) ||
    (value.comparison !== null && !isRecord(value.comparison)) ||
    !isRecord(value.meta) ||
    !isRecord(value.meta.coverage) ||
    !isRecord(value.meta.cache_coverage)
  )
    invalidResponse(endpoint)
  if (
    value.meta.source !== 'consume_logs' ||
    typeof value.meta.available !== 'boolean' ||
    value.meta.coverage.success_rate !== false ||
    value.kpi.successRate !== null
  )
    invalidResponse(endpoint)
  const coverage = value.meta.coverage
  for (const key of ['consume_logs', 'cache_usage', 'latency'])
    if (typeof coverage[key] !== 'boolean') invalidResponse(endpoint)
  return {
    kpi: {
      totalTokens: requiredStrictInteger(value.kpi.totalTokens, endpoint),
      totalQuota: requiredQuota(value.kpi.totalQuota, endpoint),
      totalRequests: requiredStrictInteger(value.kpi.totalRequests, endpoint),
      avgLatency: nullable(value.kpi.avgLatency),
      successRate: nullable(value.kpi.successRate),
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
      tokens: requiredStrictInteger(row.tokens, endpoint),
      quota: requiredQuota(row.quota, endpoint),
      requests: requiredStrictInteger(row.requests, endpoint),
      share: number(row.share),
      avgLatency: nullable(row.avgLatency),
    })),
    hourly: list(value.hourly).map((row) => ({
      hour: requiredString(row.hour, endpoint),
      requests: requiredStrictInteger(row.requests, endpoint),
    })),
    flow: list(value.flow).map((row) => ({
      date: requiredString(row.date, endpoint),
      consume: requiredQuota(row.consume, endpoint),
      requests: requiredStrictInteger(row.requests, endpoint),
    })),
    token_trend: list(value.token_trend).map((row) => ({
      date: requiredString(row.date, endpoint),
      total: requiredStrictInteger(row.total, endpoint),
      input: nullable(row.input),
      output: nullable(row.output),
      cache_create: nullable(row.cache_create),
      cache_read: nullable(row.cache_read),
      hit_rate: nullable(row.hit_rate),
    })),
    distribution: list(value.distribution).map((row) => ({
      date: requiredString(row.date, endpoint),
      requests: requiredStrictInteger(row.requests, endpoint),
      consume: requiredQuota(row.consume, endpoint),
      tokens: requiredStrictInteger(row.tokens, endpoint),
    })),
    meta: {
      start: number(value.meta.start),
      end: number(value.meta.end),
      tz_offset: number(value.meta.tz_offset),
      generated_at: number(value.meta.generated_at),
      available: value.meta.available,
      source: 'consume_logs',
      cache_coverage: {
        records: requiredStrictInteger(
          value.meta.cache_coverage.records,
          endpoint
        ),
        read_records: requiredStrictInteger(
          value.meta.cache_coverage.read_records,
          endpoint
        ),
        write_records: requiredStrictInteger(
          value.meta.cache_coverage.write_records,
          endpoint
        ),
        invalid_records: requiredStrictInteger(
          value.meta.cache_coverage.invalid_records,
          endpoint
        ),
      },
      coverage: {
        consume_logs: coverage.consume_logs as boolean,
        cache_usage: coverage.cache_usage as boolean,
        latency: coverage.latency as boolean,
        success_rate: false,
      },
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
      { tz_offset: String(new Date().getTimezoneOffset()), ...params },
      { signal }
    )
  )
}
