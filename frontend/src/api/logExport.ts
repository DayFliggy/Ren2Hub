import { api } from './client'
import { parseLogPage } from './liveContracts'
import { ApiError } from './types'
import { getLocale } from '@/i18n'
import { serverResponseMessage } from '@/i18n/serverMessages'
import type { LogItem } from '@/types/console'

// The server streams NDJSON without a total row or byte cap. Records are
// parsed one line at a time; only a malformed single line is refused.
const MAX_LOG_EXPORT_LINE_CHARS = 2 * 1024 * 1024
/** Abort when the server sends nothing for this long, before or during the body. */
export const LOG_EXPORT_IDLE_TIMEOUT_MS = 60_000
export const LOG_EXPORT_STALLED = 'LOG_EXPORT_STALLED'

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

function abortError(): DOMException {
  return new DOMException('Aborted', 'AbortError')
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

/** Checks the final record against the rows received. */
function checkTerminal(terminal: unknown, count: number): void {
  if (!isRecord(terminal) || !('success' in terminal)) throw failedExport()
  if (terminal.success !== true) {
    throw new ApiError(
      serverResponseMessage(getLocale(), terminal) || 'Failed to export logs',
      {
        business: true,
        code: typeof terminal.code === 'string' ? terminal.code : undefined,
      }
    )
  }
  if (!isRecord(terminal.data) || terminal.data.count !== count)
    throw failedExport()
}

function parseRecord(record: unknown): LogItem {
  try {
    return parseLogPage({ page: 1, page_size: 1, total: 1, items: [record] })
      .items[0]!
  } catch {
    throw failedExport()
  }
}

/**
 * Parses the NDJSON response incrementally; only the current partial line is
 * buffered. `onChunk` runs for every received chunk so callers can track idle
 * time. Aborting `signal` cancels the stream.
 */
export async function parseLogExportStream(
  stream: ReadableStream<Uint8Array>,
  signal: AbortSignal,
  onLog: (log: LogItem) => void,
  onChunk: () => void = () => undefined
): Promise<number> {
  const reader = stream.getReader()
  const cancel = () => void reader.cancel(signal.reason).catch(() => undefined)
  signal.addEventListener('abort', cancel, { once: true })
  const decoder = new TextDecoder()
  let buffer = ''
  let count = 0
  let terminal: unknown
  const consumeLine = (line: string) => {
    if (!line.trim()) return
    if (line.length > MAX_LOG_EXPORT_LINE_CHARS || terminal !== undefined)
      throw failedExport()
    let record: unknown
    try {
      record = JSON.parse(line)
    } catch {
      throw failedExport()
    }
    if (isRecord(record) && 'success' in record) {
      terminal = record
      return
    }
    onLog(parseRecord(record))
    count++
  }
  try {
    while (true) {
      if (signal.aborted) throw abortError()
      const chunk = await reader.read()
      if (signal.aborted) throw abortError()
      if (chunk.done) break
      onChunk()
      buffer += decoder.decode(chunk.value, { stream: true })
      let start = 0
      let newline = buffer.indexOf('\n')
      while (newline >= 0) {
        consumeLine(buffer.slice(start, newline))
        start = newline + 1
        newline = buffer.indexOf('\n', start)
      }
      buffer = buffer.slice(start)
      if (buffer.length > MAX_LOG_EXPORT_LINE_CHARS) throw failedExport()
    }
    consumeLine(buffer + decoder.decode())
    checkTerminal(terminal, count)
    return count
  } catch (error) {
    await reader.cancel(error).catch(() => undefined)
    throw error
  } finally {
    signal.removeEventListener('abort', cancel)
    reader.releaseLock()
  }
}

/**
 * Streams logs from `endpoint` (`/api/log/self/export` or `/api/log/export`)
 * to `onLog` and resolves with the row count once the server confirms it.
 * Fails with LOG_EXPORT_STALLED after `idleTimeoutMs` without data.
 */
export async function exportLogsFromEndpoint(
  endpoint: '/api/log/self/export' | '/api/log/export',
  filters: Record<string, unknown>,
  signal: AbortSignal,
  onLog: (log: LogItem) => void,
  idleTimeoutMs = LOG_EXPORT_IDLE_TIMEOUT_MS
): Promise<number> {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined) continue
    query.set(key, String(value))
  }
  const controller = new AbortController()
  const forwardAbort = () => controller.abort(signal.reason)
  if (signal.aborted) forwardAbort()
  else signal.addEventListener('abort', forwardAbort, { once: true })
  let stalled = false
  let timer: ReturnType<typeof setTimeout> | undefined
  const resetIdle = () => {
    clearTimeout(timer)
    timer = setTimeout(() => {
      stalled = true
      controller.abort()
    }, idleTimeoutMs)
  }
  resetIdle()
  try {
    if (controller.signal.aborted) throw abortError()
    const stream = await api.getStream(`${endpoint}?${query}`, {
      signal: controller.signal,
    })
    return await parseLogExportStream(
      stream,
      controller.signal,
      onLog,
      resetIdle
    )
  } catch (error) {
    if (stalled && !signal.aborted)
      throw new ApiError('Log export stalled', { code: LOG_EXPORT_STALLED })
    throw error
  } finally {
    clearTimeout(timer)
    signal.removeEventListener('abort', forwardAbort)
  }
}

/**
 * Streams the caller's own logs to `onLog` and resolves with the row count
 * once the server confirms it. Fails with LOG_EXPORT_STALLED after
 * `idleTimeoutMs` without data.
 */
export async function exportUserLogs(
  filters: Record<string, unknown>,
  signal: AbortSignal,
  onLog: (log: LogItem) => void,
  idleTimeoutMs = LOG_EXPORT_IDLE_TIMEOUT_MS
): Promise<number> {
  return exportLogsFromEndpoint(
    '/api/log/self/export',
    filters,
    signal,
    onLog,
    idleTimeoutMs
  )
}
