import { exportUserLogs } from '@/api/logExport'
import type { LocaleCode } from '@/i18n'
import type { LogItem } from '@/types/console'
import { createExportVolumes } from '@/utils/exportVolumes'
import { formatTime } from '@/utils/format'
import { createSpreadsheetExportStream } from '@/utils/spreadsheetExport'

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

export type LogExportType = 'csv' | 'excel' | 'json'

export interface LogExportRequest {
  filters: Record<string, unknown>
  type: LogExportType
  /** Localized column titles for CSV and Excel. */
  headers: readonly string[]
  locale: LocaleCode
  signal: AbortSignal
  /** Receives each complete file as soon as it is ready. */
  save: (blob: Blob, filename: string) => void
  volumeBytes?: number
}

function volumeFormat(type: LogExportType, headers: readonly string[]) {
  if (type === 'json')
    return {
      format: {
        prefix: '[',
        suffix: ']',
        separator: ',',
        mime: 'application/json;charset=utf-8',
      },
      ext: 'json',
      row: (log: LogItem) => JSON.stringify(log),
    }
  const spreadsheet = createSpreadsheetExportStream(headers, type)
  return {
    format: { ...spreadsheet, separator: '' },
    ext: spreadsheet.ext,
    row: (log: LogItem, locale: LocaleCode) =>
      spreadsheet.appendRow(getLogExportValues(log, locale)),
  }
}

/**
 * Streams the caller's logs into files of bounded size. Files already saved
 * stay saved when the export later fails or is cancelled; the unfinished file
 * is discarded.
 */
export async function exportLogFiles(
  request: LogExportRequest
): Promise<{ count: number; files: number }> {
  const { format, ext, row } = volumeFormat(request.type, request.headers)
  const base = `ren2hub-logs-${new Date().toISOString().slice(0, 10)}`
  const volumes = createExportVolumes(
    format,
    (blob, part) =>
      request.save(
        blob,
        `${base}${part === null ? '' : `-part${part}`}.${ext}`
      ),
    request.volumeBytes
  )
  const count = await exportUserLogs(request.filters, request.signal, (log) =>
    volumes.append(row(log, request.locale))
  )
  return { count, files: volumes.finish() }
}
