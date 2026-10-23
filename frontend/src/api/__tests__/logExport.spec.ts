import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
  exportUserLogs,
  MAX_LOG_EXPORT_BYTES,
  parseLogExport,
  parseLogExportStream,
} from '@/api/logExport'

const row = {
  id: 1,
  type: 2,
  token_name: 'fixture',
  model_name: 'model',
  channel_name: 'channel',
  quota: 9007199254740991,
  prompt_tokens: 1,
  completion_tokens: 2,
  use_time: 1,
  is_stream: true,
  content: '=untrusted',
  created_at: 1700000000,
}
const terminal = { success: true, data: { count: 1 } }
const stream = `${JSON.stringify(row)}\n${JSON.stringify(terminal)}`
function ndjsonStream(text: string): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(text))
      controller.close()
    },
  })
}

afterEach(() => vi.restoreAllMocks())

describe('usage log export', () => {
  it('requires a successful terminal record matching the complete row count', () => {
    expect(parseLogExport(stream)[0]).toMatchObject({
      quota: '9007199254740991',
      content: '=untrusted',
    })
    expect(parseLogExport('{"success":true,"data":{"count":0}}')).toEqual([])
    for (const value of [
      JSON.stringify(row),
      `${JSON.stringify(row)}\n`,
      `${stream}\n{`,
      `${JSON.stringify(row)}\n{"success":true,"data":{"count":2}}`,
    ]) {
      expect(() => parseLogExport(value)).toThrow()
    }
    expect(() =>
      parseLogExport(
        `${JSON.stringify(row)}\n{"success":false,"message":"query failed"}`
      )
    ).toThrow('query failed')
  })
  it('uses the dedicated endpoint with active filters and cancellation', async () => {
    const get = vi
      .spyOn(api, 'getBlob')
      .mockResolvedValue({ text: async () => stream } as Blob)
    const controller = new AbortController()
    expect(
      await exportUserLogs(
        { keyword: 'model & tool', group: 'past', type: 2 },
        controller.signal
      )
    ).toHaveLength(1)
    const [url, options] = get.mock.calls[0]!
    const query = new URL(url, 'https://ren2hub.invalid')
    expect(query.pathname).toBe('/api/log/self/export')
    expect(query.searchParams.get('keyword')).toBe('model & tool')
    expect(query.searchParams.get('group')).toBe('past')
    expect(options?.signal).toBe(controller.signal)
    expect(options?.maxBytes).toBe(MAX_LOG_EXPORT_BYTES)
    get.mockResolvedValueOnce({ text: async () => stream } as Blob)
    const collected: unknown[] = []
    expect(
      await exportUserLogs({ type: 2 }, controller.signal, (log) =>
        collected.push(log)
      )
    ).toBe(1)
    expect(collected).toHaveLength(1)
    controller.abort()
    await expect(exportUserLogs({}, controller.signal)).rejects.toMatchObject({
      name: 'AbortError',
    })
  })

  it('parses streamed records incrementally and requires a matching final count', async () => {
    const collected: unknown[] = []
    expect(
      await parseLogExportStream(
        ndjsonStream(stream),
        new AbortController().signal,
        (log) => collected.push(log)
      )
    ).toBe(1)
    expect(collected).toHaveLength(1)
    await expect(
      parseLogExportStream(
        ndjsonStream(JSON.stringify(row)),
        new AbortController().signal,
        () => undefined
      )
    ).rejects.toThrow()
    await expect(
      parseLogExportStream(
        ndjsonStream(
          `${JSON.stringify(row)}\n${JSON.stringify({ success: true, data: { count: 2 } })}`
        ),
        new AbortController().signal,
        () => undefined
      )
    ).rejects.toThrow()
  })

  it('rejects a response above the client memory bound before parsing it', async () => {
    const get = vi.spyOn(api, 'getBlob').mockResolvedValue({
      size: MAX_LOG_EXPORT_BYTES + 1,
      text: vi.fn(),
    } as unknown as Blob)
    await expect(
      exportUserLogs({}, new AbortController().signal, () => undefined)
    ).rejects.toMatchObject({ code: 'LOG_EXPORT_TOO_LARGE' })
    expect(get).toHaveBeenCalledOnce()
    expect(get.mock.results[0]?.value).toBeDefined()
  })

  it('accepts large chunks containing many short records', async () => {
    const count = 10_000
    const ndjson = `${(JSON.stringify(row) + '\n').repeat(count)}${JSON.stringify({ success: true, data: { count } })}`
    expect(ndjson.length).toBeGreaterThan(2 * 1024 * 1024)
    await expect(
      parseLogExportStream(
        ndjsonStream(ndjson),
        new AbortController().signal,
        () => undefined
      )
    ).resolves.toBe(count)
  })

  it('cancels an invalid stream and refuses further rows', async () => {
    const cancel = vi.fn()
    const invalid = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('invalid\n'))
      },
      cancel,
    })
    await expect(
      parseLogExportStream(
        invalid,
        new AbortController().signal,
        () => undefined
      )
    ).rejects.toMatchObject({ code: 'INVALID_LOG_EXPORT' })
    expect(cancel).toHaveBeenCalledOnce()
    expect(invalid.locked).toBe(false)
  })

  it('enforces the total byte limit while reading a stream', async () => {
    const cancel = vi.fn()
    const oversized = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array(MAX_LOG_EXPORT_BYTES + 1))
      },
      cancel,
    })
    await expect(
      parseLogExportStream(
        oversized,
        new AbortController().signal,
        () => undefined
      )
    ).rejects.toMatchObject({ code: 'LOG_EXPORT_TOO_LARGE' })
    expect(cancel).toHaveBeenCalledOnce()
  })
})
