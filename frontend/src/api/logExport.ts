import { api } from './client'
import { parseLogPage } from './liveContracts'
import { ApiError } from './types'
import { getLocale } from '@/i18n'
import { serverResponseMessage } from '@/i18n/serverMessages'
import type { LogItem } from '@/types/console'

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

/** A file is downloadable only after the server's successful final record. */
export function parseLogExport(text: string): LogItem[] {
  const lines = text.trim().split('\n')
  let records: unknown[]
  try {
    records = lines.map((line) => JSON.parse(line) as unknown)
  } catch {
    throw failedExport()
  }
  const terminal = records.pop()
  if (!terminal || typeof terminal !== 'object' || !('success' in terminal))
    throw failedExport()
  if (terminal.success !== true) {
    throw new ApiError(
      serverResponseMessage(getLocale(), terminal) || 'Failed to export logs',
      { business: true }
    )
  }
  if (
    !('data' in terminal) ||
    !terminal.data ||
    typeof terminal.data !== 'object' ||
    !('count' in terminal.data) ||
    terminal.data.count !== records.length
  )
    throw failedExport()
  return parseLogPage({
    page: 1,
    page_size: Math.max(1, records.length),
    total: records.length,
    items: records,
  }).items
}

export async function exportUserLogs(
  filters: Record<string, unknown>,
  signal: AbortSignal
): Promise<LogItem[]> {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filters))
    query.set(key, String(value))
  const blob = await api.getBlob(`/api/log/self/export?${query}`, {
    signal,
    timeoutMs: 120_000,
  })
  const text = await blob.text()
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
  return parseLogExport(text)
}
