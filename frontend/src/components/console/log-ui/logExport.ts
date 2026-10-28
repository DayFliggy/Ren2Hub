import type { LocaleCode } from '@/i18n'
import type { LogItem } from '@/types/console'
import { formatTime } from '@/utils/format'

import { logContentText, logTokenName } from './logContent'
import { getLogMetadata } from './logMetadata'
import { getLogUsageSummary } from './logUsage'

export const LOG_EXPORT_HEADERS = [
  'time',
  'token',
  'type',
  'model',
  'channel',
  'reasoning_effort',
  'request_mode',
  'first_token_latency',
  'prompt_tokens',
  'completion_tokens',
  'cache_read_tokens',
  'cache_write_tokens',
  'cache_ttl',
  'latency',
  'tps',
  'quota',
  'content',
  'cache_write_tokens_12h',
] as const

export function getLogExportValues(
  log: LogItem,
  locale: LocaleCode
): Array<string | number> {
  const reasoningEffort = getLogMetadata(log).reasoningEffort
  const usage = getLogUsageSummary(log)
  return [
    formatTime(log.created),
    logTokenName(log, locale),
    log.type,
    log.model,
    log.channel,
    reasoningEffort ?? '',
    log.request_mode ?? '',
    log.first_token_latency ?? '',
    log.prompt_tokens,
    log.completion_tokens,
    usage.cacheReadTokens ?? '',
    usage.cacheWriteTokens ?? '',
    log.cache_ttl ?? '',
    log.latency,
    log.tps,
    log.quota,
    logContentText(log, locale),
    usage.cacheWrite12hTokens ?? '',
  ]
}
