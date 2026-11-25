import {
  getAuthSessionSnapshot,
  isAuthSessionIdentityCurrent,
} from '@/api/authSession'
import { exportLogsFromEndpoint, exportUserLogs } from '@/api/logExport'
import { ApiError } from '@/api/types'
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
export const LOG_EXPORT_SESSION_CHANGED = 'LOG_EXPORT_SESSION_CHANGED'

/** Local 'YYYY-MM-DD' days; an empty string leaves that side open. */
export interface LogExportRange {
  start: string
  end: string
}

export interface LogExportProgress {
  count: number
  files: number
}

export interface LogExportRequest {
  filters: Record<string, unknown>
  range: LogExportRange
  type: LogExportType
  /** Localized column titles for CSV and Excel. */
  headers: readonly string[]
  locale: LocaleCode
  signal: AbortSignal
  /** Receives each complete file as soon as it is ready. */
  save: (blob: Blob, filename: string) => void
  /** Called at most once per animation frame while the export runs. */
  onProgress?: (progress: LogExportProgress) => void
  volumeBytes?: number
  /** Defaults to the personal export stream; admins may export `/api/log/export`. */
  endpoint?: '/api/log/self/export' | '/api/log/export'
  /** Filename prefix; defaults to `ren2hub-logs`. */
  filenamePrefix?: string
}

function compactLocalDay(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}`
}

/** <prefix>-<start|all>-<end|today>[-partN].<ext>, in local dates. */
export function logExportFilename(
  range: LogExportRange,
  ext: string,
  part: number | null,
  today = new Date(),
  prefix = 'ren2hub-logs'
): string {
  const start = range.start ? range.start.replaceAll('-', '') : 'all'
  const end = range.end ? range.end.replaceAll('-', '') : compactLocalDay(today)
  const suffix = part === null ? '' : `-part${part}`
  return `${prefix}-${start}-${end}${suffix}.${ext}`
}

/**
 * Coalesces per-row progress into one report per animation frame, so a
 * reactive update does not run for every parsed row.
 */
function createProgressReporter(report: (progress: LogExportProgress) => void) {
  const progress: LogExportProgress = { count: 0, files: 0 }
  let frame: number | null = null
  const schedule = () => {
    frame ??= requestAnimationFrame(() => {
      frame = null
      report({ ...progress })
    })
  }
  return {
    row() {
      progress.count++
      schedule()
    },
    file() {
      progress.files++
      schedule()
    },
    stop() {
      if (frame !== null) cancelAnimationFrame(frame)
      frame = null
    },
  }
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
 * stay saved when the export later fails, is cancelled or the sign-in
 * changes; the unfinished file is discarded.
 */
export async function exportLogFiles(
  request: LogExportRequest
): Promise<{ count: number; files: number }> {
  const { format, ext, row } = volumeFormat(request.type, request.headers)
  const today = new Date()
  // Files of one account must not be saved after another one signed in.
  const session = getAuthSessionSnapshot()
  const progress = createProgressReporter(
    request.onProgress ?? (() => undefined)
  )
  const volumes = createExportVolumes(
    format,
    (blob, part) => {
      if (!isAuthSessionIdentityCurrent(session))
        throw new ApiError('Sign-in changed during the log export', {
          code: LOG_EXPORT_SESSION_CHANGED,
        })
      request.save(
        blob,
        logExportFilename(
          request.range,
          ext,
          part,
          today,
          request.filenamePrefix ?? 'ren2hub-logs'
        )
      )
      progress.file()
    },
    request.volumeBytes
  )
  try {
    const endpoint = request.endpoint ?? '/api/log/self/export'
    const onRow = (log: LogItem) => {
      volumes.append(row(log, request.locale))
      progress.row()
    }
    const count =
      endpoint === '/api/log/self/export'
        ? await exportUserLogs(request.filters, request.signal, onRow)
        : await exportLogsFromEndpoint(
            endpoint,
            request.filters,
            request.signal,
            onRow
          )
    return { count, files: volumes.finish() }
  } finally {
    progress.stop()
  }
}
