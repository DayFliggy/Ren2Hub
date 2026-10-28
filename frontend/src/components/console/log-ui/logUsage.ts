import type { LogItem } from '@/types/console'
import { logEvidence } from '@/utils/logModel'

export interface LogUsageSummary {
  available: boolean
  promptTokens: number
  completionTokens: number
  cacheReadTokens: number | null
  cacheWriteTokens: number | null
  cacheWrite12hTokens: number | null
  cacheWrite30mTokens: number | null
  cacheTtl: string | null
  totalTokens: number
  cacheHitRate: number | null
}

function normalizeTokenCount(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0)
    return null
  return Math.round(value)
}

function requestTokenCount(value: number): number {
  return normalizeTokenCount(value) ?? 0
}

export function getLogUsageSummary(log: LogItem): LogUsageSummary {
  if (log.request_mode === null) {
    return {
      available: false,
      promptTokens: 0,
      completionTokens: 0,
      cacheReadTokens: null,
      cacheWriteTokens: null,
      cacheWrite12hTokens: null,
      cacheWrite30mTokens: null,
      cacheTtl: null,
      totalTokens: 0,
      cacheHitRate: null,
    }
  }

  const promptTokens = requestTokenCount(log.prompt_tokens)
  const completionTokens = requestTokenCount(log.completion_tokens)
  const other = logEvidence(log)
  const cacheReadTokens = normalizeTokenCount(
    log.cache_read_tokens ?? other.cache_read_tokens ?? other.cache_tokens
  )
  const cacheWrite12hTokensDetail = normalizeTokenCount(
    log.cache_creation_tokens_12h ?? other.cache_creation_tokens_12h
  )
  const cacheWriteTokens = normalizeTokenCount(
    log.cache_write_tokens ??
      other.cache_write_tokens ??
      other.cache_creation_tokens ??
      cacheWrite12hTokensDetail
  )
  const cacheWrite12hTokens =
    cacheWrite12hTokensDetail === null
      ? null
      : cacheWriteTokens === null
        ? cacheWrite12hTokensDetail
        : Math.min(cacheWrite12hTokensDetail, cacheWriteTokens)
  const cacheWrite30mTokens =
    cacheWriteTokens === null || cacheWrite12hTokens === null
      ? cacheWriteTokens
      : Math.max(cacheWriteTokens - cacheWrite12hTokens, 0)
  const isAnthropic = other.usage_semantic === 'anthropic'
  const inputTokens =
    normalizeTokenCount(other.input_tokens_total) ??
    promptTokens +
      (isAnthropic ? (cacheReadTokens ?? 0) + (cacheWriteTokens ?? 0) : 0)
  const totalTokens = inputTokens + completionTokens
  const cacheTtl = log.cache_ttl?.trim() || null

  return {
    available: true,
    promptTokens,
    completionTokens,
    cacheReadTokens,
    cacheWriteTokens,
    cacheWrite12hTokens,
    cacheWrite30mTokens,
    cacheTtl,
    totalTokens,
    cacheHitRate:
      cacheReadTokens === null || inputTokens === 0
        ? null
        : (cacheReadTokens / inputTokens) * 100,
  }
}

export function formatLogCacheHitRate(value: number | null): string {
  return value === null || !Number.isFinite(value)
    ? '—'
    : `${value.toFixed(2)}%`
}
