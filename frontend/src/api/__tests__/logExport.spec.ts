import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { setLocale } from '@/i18n'
// Response of a real export rejected at the global concurrency limit.
import busy from '../../../tests/fixtures/native/log-export-busy.json'
import {
  exportUserLogs,
  LOG_EXPORT_STALLED,
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
const ndjson = `${JSON.stringify(row)}\n${JSON.stringify(terminal)}`

function chunkedStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
      controller.close()
    },
  })
}

function parse(text: string, onLog: (log: unknown) => void = () => undefined) {
  return parseLogExportStream(
    chunkedStream([text]),
    new AbortController().signal,
    onLog
  )
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('usage log export stream', () => {
  it('requires a successful terminal record matching the complete row count', async () => {
    const collected: unknown[] = []
    await expect(parse(ndjson, (log) => collected.push(log))).resolves.toBe(1)
    expect(collected[0]).toMatchObject({
      quota: '9007199254740991',
      content: '=untrusted',
    })
    await expect(parse('{"success":true,"data":{"count":0}}')).resolves.toBe(0)
    for (const value of [
      JSON.stringify(row),
      `${JSON.stringify(row)}\n`,
      `${ndjson}\n{`,
      `${ndjson}\n${JSON.stringify(row)}`,
      `${JSON.stringify(row)}\n{"success":true,"data":{"count":2}}`,
    ]) {
      await expect(parse(value)).rejects.toMatchObject({
        code: 'INVALID_LOG_EXPORT',
      })
    }
    await expect(
      parse(
        `${JSON.stringify(row)}\n{"success":false,"message":"query failed"}`
      )
    ).rejects.toThrow('query failed')
  })

  it('reassembles records split across chunks, including multibyte text', async () => {
    const text = `${JSON.stringify({ ...row, content: '中文内容' })}\n${JSON.stringify(terminal)}`
    const bytes = new TextEncoder().encode(text)
    const split = bytes.indexOf(0xe4) + 1
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(bytes.slice(0, split))
        controller.enqueue(bytes.slice(split))
        controller.close()
      },
    })
    const collected: { content?: string }[] = []
    await expect(
      parseLogExportStream(stream, new AbortController().signal, (log) =>
        collected.push(log)
      )
    ).resolves.toBe(1)
    expect(collected[0]?.content).toBe('中文内容')
  })

  it('exports more rows than the former browser cap without a total limit', async () => {
    const count = 120_000
    const line = JSON.stringify(row) + '\n'
    // 1 000 rows per chunk keeps the stream fast while crossing every old cap.
    const chunks = Array.from({ length: count / 1000 }, () => line.repeat(1000))
    chunks.push(JSON.stringify({ success: true, data: { count } }))
    let received = 0
    await expect(
      parseLogExportStream(
        chunkedStream(chunks),
        new AbortController().signal,
        () => received++
      )
    ).resolves.toBe(count)
    expect(received).toBe(count)
  })

  it('rejects a single line over 2 MiB as an invalid export and cancels the stream', async () => {
    const cancel = vi.fn()
    const oversized = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          new TextEncoder().encode(`"${'x'.repeat(2 * 1024 * 1024)}`)
        )
      },
      cancel,
    })
    await expect(
      parseLogExportStream(
        oversized,
        new AbortController().signal,
        () => undefined
      )
    ).rejects.toMatchObject({ code: 'INVALID_LOG_EXPORT' })
    expect(cancel).toHaveBeenCalledOnce()
    expect(oversized.locked).toBe(false)
  })

  it('reports a busy server with its code and a localized message', async () => {
    await setLocale('zh-CN')
    await expect(parse(JSON.stringify(busy))).rejects.toMatchObject({
      business: true,
      code: 'LOG_EXPORT_BUSY',
      message: '导出任务繁忙，请稍后再试。',
    })
    await setLocale('en')
    await expect(parse(JSON.stringify(busy))).rejects.toMatchObject({
      code: 'LOG_EXPORT_BUSY',
      message: 'Log export is busy. Try again later.',
    })
  })
})

describe('usage log export request', () => {
  it('streams the dedicated endpoint with active filters', async () => {
    const get = vi
      .spyOn(api, 'getStream')
      .mockResolvedValue(chunkedStream([ndjson]))
    const collected: unknown[] = []
    await expect(
      exportUserLogs(
        { keyword: 'model & tool', type: 2 },
        new AbortController().signal,
        (log) => collected.push(log)
      )
    ).resolves.toBe(1)
    expect(collected).toHaveLength(1)
    const [url, options] = get.mock.calls[0]!
    const query = new URL(url, 'https://ren2hub.invalid')
    expect(query.pathname).toBe('/api/log/self/export')
    expect(query.searchParams.get('keyword')).toBe('model & tool')
    expect(query.searchParams.get('type')).toBe('2')
    expect(options?.signal?.aborted).toBe(false)
  })

  it('cancels the request and the stream when the caller aborts', async () => {
    const cancel = vi.fn()
    let pull!: () => void
    const pulled = new Promise<void>((resolve) => (pull = resolve))
    const stream = new ReadableStream<Uint8Array>({
      pull: () => pull(),
      cancel,
    })
    const get = vi.spyOn(api, 'getStream').mockResolvedValue(stream)
    const controller = new AbortController()
    const done = exportUserLogs({}, controller.signal, () => undefined)
    await pulled
    controller.abort()
    await expect(done).rejects.toMatchObject({ name: 'AbortError' })
    expect(get.mock.calls[0]?.[1]?.signal?.aborted).toBe(true)
    expect(cancel).toHaveBeenCalledOnce()
  })

  it('aborts after the idle timeout while rows keep the timer alive', async () => {
    vi.useFakeTimers()
    const encoder = new TextEncoder()
    let source!: ReadableStreamDefaultController<Uint8Array>
    const cancel = vi.fn()
    vi.spyOn(api, 'getStream').mockResolvedValue(
      new ReadableStream<Uint8Array>({
        start: (controller) => void (source = controller),
        cancel,
      })
    )
    let received = 0
    const done = exportUserLogs(
      {},
      new AbortController().signal,
      () => received++,
      1000
    )
    const outcome = done.catch((error: unknown) => error)
    for (let index = 0; index < 3; index++) {
      await vi.advanceTimersByTimeAsync(800)
      source.enqueue(encoder.encode(JSON.stringify(row) + '\n'))
    }
    await vi.advanceTimersByTimeAsync(800)
    expect(received).toBe(3)
    expect(cancel).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(300)
    expect(await outcome).toMatchObject({ code: LOG_EXPORT_STALLED })
    expect(cancel).toHaveBeenCalledOnce()
  })

  it('applies the idle timeout while waiting for the response headers', async () => {
    vi.useFakeTimers()
    let requestSignal: AbortSignal | undefined
    vi.spyOn(api, 'getStream').mockImplementation(
      (_url, options) =>
        new Promise((_, reject) => {
          requestSignal = options?.signal
          options?.signal?.addEventListener('abort', () =>
            reject(new DOMException('Aborted', 'AbortError'))
          )
        })
    )
    const outcome = exportUserLogs(
      {},
      new AbortController().signal,
      () => undefined,
      1000
    ).catch((error: unknown) => error)
    await vi.advanceTimersByTimeAsync(1001)
    expect(requestSignal?.aborted).toBe(true)
    expect(await outcome).toMatchObject({ code: LOG_EXPORT_STALLED })
  })
})
