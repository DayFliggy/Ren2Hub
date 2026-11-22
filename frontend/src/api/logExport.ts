import { api } from './client'
import { parseLogPage } from './liveContracts'
import { ApiError } from './types'
import { getLocale } from '@/i18n'
import { serverResponseMessage } from '@/i18n/serverMessages'
import type { LogItem } from '@/types/console'

// The server deliberately streams NDJSON and has no upstream row/byte cap.
// Bound parsing and output, and avoid retaining the entire response as both
// text and parsed rows alongside the spreadsheet.
export const MAX_LOG_EXPORT_BYTES = 64 * 1024 * 1024
export const MAX_LOG_EXPORT_ROWS = 100_000
const MAX_LOG_EXPORT_LINE_CHARS = 2 * 1024 * 1024

function failedExport(): ApiError {
  return new ApiError(
    serverResponseMessage(getLocale(), {
      message_key: 'Incomplete or invalid log export',
    }) || 'Incomplete or invalid log export',
    {
      code: 'INVALID_LOG_EXPORT',
    }
  )
}

function tooLargeExport(): ApiError {
  return new ApiError('Log export is too large for the browser', {
    code: 'LOG_EXPORT_TOO_LARGE',
  })
}

const LIMIT_CODE = 'LOG_EXPORT_LIMIT_EXCEEDED'

/** The server stopped at its row or byte cap after sending `count` rows. */
export type LogExportLimit = {
  count: number
  maxRows: number
  maxBytes: number
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function isCount(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) >= 0
}

/**
 * Checks the final record against the rows received. A capped export throws
 * a LOG_EXPORT_LIMIT_EXCEEDED error whose rows are the first `count` records.
 */
function checkTerminal(terminal: unknown, count: number): void {
  if (!isRecord(terminal) || !('success' in terminal)) throw failedExport()
  if (terminal.success !== true && terminal.code === LIMIT_CODE) {
    const data = terminal.data
    if (
      !isRecord(data) ||
      data.truncated !== true ||
      data.count !== count ||
      !isCount(data.max_rows) ||
      !isCount(data.max_bytes)
    )
      throw failedExport()
    const limit: LogExportLimit = {
      count,
      maxRows: data.max_rows,
      maxBytes: data.max_bytes,
    }
    throw new ApiError(
      serverResponseMessage(getLocale(), terminal) ||
        'Log export limit exceeded',
      { business: true, code: LIMIT_CODE, data: limit }
    )
  }
  if (terminal.success !== true) {
    throw new ApiError(
      serverResponseMessage(getLocale(), terminal) || 'Failed to export logs',
      { business: true }
    )
  }
  if (!isRecord(terminal.data) || terminal.data.count !== count)
    throw failedExport()
}

/** Parameters of the logs.exportLimited message. */
export function logExportLimitParams(limit: LogExportLimit) {
  return {
    count: limit.count,
    rows: limit.maxRows,
    size: Math.floor(limit.maxBytes / (1024 * 1024)),
  }
}

export function logExportLimit(error: unknown): LogExportLimit | null {
  return error instanceof ApiError && error.code === LIMIT_CODE
    ? (error.data as LogExportLimit)
    : null
}

/** Validate records only after the successful final record has arrived. */
function parseExportRecords(records: unknown[]): LogItem[] {
  const terminal = records.pop()
  checkTerminal(terminal, records.length)
  return parseLogPage({
    page: 1,
    page_size: Math.max(1, records.length),
    total: records.length,
    items: records,
  }).items
}

/** A file is downloadable only after the server's successful final record. */
export function parseLogExport(text: string): LogItem[] {
  if (text.length > MAX_LOG_EXPORT_BYTES) throw tooLargeExport()
  const trimmed = text.trim()
  if (!trimmed) throw failedExport()
  let records: unknown[]
  try {
    records = trimmed.split('\n').map((line) => JSON.parse(line) as unknown)
  } catch {
    throw failedExport()
  }
  if (records.length - 1 > MAX_LOG_EXPORT_ROWS) throw tooLargeExport()
  return parseExportRecords(records)
}

/** Parse the NDJSON response incrementally without creating a full text copy. */
export async function parseLogExportStream(
  stream: ReadableStream<Uint8Array>,
  signal: AbortSignal,
  onLog: (log: LogItem) => void
): Promise<number> {
  const reader = stream.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let count = 0
  let bytes = 0
  let terminal: unknown
  const consumeLine = (line: string) => {
    if (!line.trim()) return
    if (line.length > MAX_LOG_EXPORT_LINE_CHARS) throw tooLargeExport()
    if (terminal !== undefined) throw failedExport()
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    try {
      const record = JSON.parse(line) as unknown
      if (record && typeof record === 'object' && 'success' in record) {
        terminal = record
        return
      }
      if (count >= MAX_LOG_EXPORT_ROWS) throw tooLargeExport()
      const [log] = parseLogPage({
        page: 1,
        page_size: 1,
        total: 1,
        items: [record],
      }).items
      onLog(log!)
      count++
    } catch (error) {
      if (error instanceof ApiError && error.code === 'LOG_EXPORT_TOO_LARGE')
        throw error
      throw failedExport()
    }
  }
  try {
    while (true) {
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
      const chunk = await reader.read()
      if (chunk.done) break
      bytes += chunk.value.byteLength
      if (bytes > MAX_LOG_EXPORT_BYTES) throw tooLargeExport()
      buffer += decoder.decode(chunk.value, { stream: true })
      let newline = buffer.indexOf('\n')
      while (newline >= 0) {
        consumeLine(buffer.slice(0, newline))
        buffer = buffer.slice(newline + 1)
        newline = buffer.indexOf('\n')
      }
      if (buffer.length > MAX_LOG_EXPORT_LINE_CHARS) throw tooLargeExport()
    }
    buffer += decoder.decode()
    consumeLine(buffer)
    checkTerminal(terminal, count)
    return count
  } catch (error) {
    await reader.cancel(error)
    throw error
  } finally {
    reader.releaseLock()
  }
}

export function exportUserLogs(
  filters: Record<string, unknown>,
  signal: AbortSignal
): Promise<LogItem[]>
export function exportUserLogs(
  filters: Record<string, unknown>,
  signal: AbortSignal,
  onLog: (log: LogItem) => void
): Promise<number>
export async function exportUserLogs(
  filters: Record<string, unknown>,
  signal: AbortSignal,
  onLog?: (log: LogItem) => void
): Promise<LogItem[] | number> {
  const collected: LogItem[] = []
  const emit = onLog ?? ((log: LogItem) => collected.push(log))
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filters))
    query.set(key, String(value))
  const blob = await api
    .getBlob(`/api/log/self/export?${query}`, {
      signal,
      timeoutMs: 120_000,
      maxBytes: MAX_LOG_EXPORT_BYTES,
    })
    .catch((error: unknown) => {
      if (
        error instanceof ApiError &&
        error.code === 'BINARY_DOWNLOAD_TOO_LARGE'
      )
        throw tooLargeExport()
      throw error
    })
  if (blob.size > MAX_LOG_EXPORT_BYTES) throw tooLargeExport()
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
  if (typeof blob.stream === 'function')
    return onLog
      ? parseLogExportStream(blob.stream(), signal, emit)
      : parseLogExportStream(blob.stream(), signal, emit).then(() => collected)
  // Older test transports may expose only text(); production browsers support
  // Blob.stream(), while this fallback preserves transport compatibility.
  const text = await blob.text()
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
  const logs = parseLogExport(text)
  for (const log of logs) emit(log)
  return onLog ? logs.length : collected
}
