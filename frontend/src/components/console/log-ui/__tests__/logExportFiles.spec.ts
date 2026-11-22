import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/client'
import {
  exportLogFiles,
  logExportFilename,
} from '@/components/console/log-ui/logExport'

// jsdom's Blob has no text(); FileReader is the browser API it does provide.
function text(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = () => reject(reader.error)
    reader.readAsText(blob)
  })
}

function bytes(blob: Blob): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer))
    reader.onerror = () => reject(reader.error)
    reader.readAsArrayBuffer(blob)
  })
}

function record(id: number) {
  return {
    id,
    type: 2,
    token_name: 'fixture',
    model_name: `model-${id}`,
    channel_name: 'channel',
    quota: 1,
    prompt_tokens: 1,
    completion_tokens: 2,
    use_time: 1,
    is_stream: true,
    content: '=untrusted',
    created_at: 1700000000,
  }
}

function rows(count: number): string[] {
  return Array.from({ length: count }, (_, index) =>
    JSON.stringify(record(index + 1))
  )
}

function streamOf(lines: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const line of lines) controller.enqueue(encoder.encode(line + '\n'))
      controller.close()
    },
  })
}

const success = (count: number) =>
  JSON.stringify({ success: true, data: { count } })

async function run(
  type: 'csv' | 'excel' | 'json',
  lines: string[],
  volumeBytes = 1200
) {
  vi.spyOn(api, 'getStream').mockResolvedValue(streamOf(lines))
  const files: { name: string; blob: Blob }[] = []
  const outcome = exportLogFiles({
    filters: {},
    range: { start: '2026-07-01', end: '2026-07-31' },
    type,
    headers: ['时间', '模型'],
    locale: 'zh-CN',
    signal: new AbortController().signal,
    save: (blob, name) => files.push({ name, blob }),
    volumeBytes,
  })
  return { files, outcome }
}

afterEach(() => vi.restoreAllMocks())

describe('log export files', () => {
  it('keeps the plain filename when everything fits in one file', async () => {
    const { files, outcome } = await run('json', [...rows(2), success(2)])
    await expect(outcome).resolves.toEqual({ count: 2, files: 1 })
    expect(files.map((file) => file.name)).toEqual([
      'ren2hub-logs-20260701-20260731.json',
    ])
    const parsed = JSON.parse(await text(files[0]!.blob)) as { id: number }[]
    expect(parsed.map((log) => log.id)).toEqual([1, 2])
  })

  it('splits JSON into numbered files that are each a valid array', async () => {
    const { files, outcome } = await run('json', [...rows(10), success(10)])
    const { count, files: total } = await outcome
    expect(count).toBe(10)
    expect(total).toBeGreaterThan(1)
    expect(files.map((file) => file.name)).toEqual(
      Array.from(
        { length: total },
        (_, index) => `ren2hub-logs-20260701-20260731-part${index + 1}.json`
      )
    )
    const ids: number[] = []
    for (const file of files) {
      expect(file.blob.size).toBeLessThanOrEqual(1200)
      const parsed = JSON.parse(await text(file.blob)) as { id: number }[]
      ids.push(...parsed.map((log) => log.id))
    }
    expect(ids).toEqual(Array.from({ length: 10 }, (_, index) => index + 1))
  })

  it('starts every CSV file with the BOM and the header row', async () => {
    const { files, outcome } = await run('csv', [...rows(20), success(20)], 400)
    const { files: total } = await outcome
    expect(total).toBeGreaterThan(1)
    const dataRows: string[] = []
    for (const [index, file] of files.entries()) {
      expect(file.name).toMatch(new RegExp(`-part${index + 1}\\.csv$`))
      // FileReader drops the BOM while decoding, so check the raw bytes.
      expect([...(await bytes(file.blob)).slice(0, 3)]).toEqual([
        0xef, 0xbb, 0xbf,
      ])
      const [header, ...body] = (await text(file.blob)).split('\n')
      expect(header).toBe('"时间","模型"')
      expect(body.length).toBeGreaterThan(0)
      dataRows.push(...body)
    }
    expect(dataRows).toHaveLength(20)
    expect(dataRows.every((line) => line.includes("'=untrusted"))).toBe(true)
  })

  it('makes every Excel file a complete HTML table', async () => {
    const { files, outcome } = await run(
      'excel',
      [...rows(12), success(12)],
      900
    )
    const { files: total } = await outcome
    expect(total).toBeGreaterThan(1)
    let bodyRows = 0
    for (const file of files) {
      expect(file.name).toMatch(/-part\d+\.xls$/)
      const content = await text(file.blob)
      expect(content).toMatch(
        /^<html><head><meta charset="utf-8"><\/head><body><table><tr><th>时间<\/th><th>模型<\/th><\/tr>.*<\/table><\/body><\/html>$/
      )
      bodyRows +=
        content.split('<td>').length > 1
          ? content.match(/<tr><td>/g)!.length
          : 0
    }
    expect(bodyRows).toBe(12)
  })

  it('keeps saved files and saves nothing more when the server fails mid-way', async () => {
    const { files, outcome } = await run('json', [
      ...rows(10),
      JSON.stringify({ success: false, message: 'query failed' }),
    ])
    await expect(outcome).rejects.toThrow('query failed')
    expect(files.length).toBeGreaterThan(0)
    for (const file of files) {
      expect(file.name).toMatch(/-part\d+\.json$/)
      expect(Array.isArray(JSON.parse(await text(file.blob)))).toBe(true)
    }
  })

  it('stops saving files once the caller cancels', async () => {
    const encoder = new TextEncoder()
    let source!: ReadableStreamDefaultController<Uint8Array>
    vi.spyOn(api, 'getStream').mockResolvedValue(
      new ReadableStream({ start: (controller) => void (source = controller) })
    )
    const controller = new AbortController()
    const files: string[] = []
    const outcome = exportLogFiles({
      filters: {},
      range: { start: '', end: '' },
      type: 'json',
      headers: [],
      locale: 'en',
      signal: controller.signal,
      save: (_blob, name) => files.push(name),
      volumeBytes: 600,
    }).catch((error: unknown) => error)
    for (const line of rows(4)) source.enqueue(encoder.encode(line + '\n'))
    await vi.waitFor(() => expect(files.length).toBeGreaterThan(0))
    const saved = files.length
    controller.abort()
    expect(await outcome).toMatchObject({ name: 'AbortError' })
    expect(files).toHaveLength(saved)
  })

  it('never reports per row and stops reporting once the export ends', async () => {
    vi.useFakeTimers({
      toFake: ['requestAnimationFrame', 'cancelAnimationFrame'],
    })
    try {
      vi.spyOn(api, 'getStream').mockResolvedValue(
        streamOf([...rows(1200), success(1200)])
      )
      const progress: { count: number; files: number }[] = []
      const outcome = exportLogFiles({
        filters: {},
        range: { start: '', end: '' },
        type: 'json',
        headers: [],
        locale: 'en',
        signal: new AbortController().signal,
        save: () => undefined,
        onProgress: (value) => progress.push({ ...value }),
        volumeBytes: 100_000,
      })
      await expect(outcome).resolves.toMatchObject({ count: 1200 })
      // 1 200 parsed rows produced no synchronous updates.
      expect(progress).toEqual([])
      vi.advanceTimersToNextFrame()
      expect(progress).toEqual([])
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows the latest count and saved files on the next frame', async () => {
    vi.useFakeTimers({
      toFake: ['requestAnimationFrame', 'cancelAnimationFrame'],
    })
    try {
      const encoder = new TextEncoder()
      let source!: ReadableStreamDefaultController<Uint8Array>
      vi.spyOn(api, 'getStream').mockResolvedValue(
        new ReadableStream({
          start: (controller) => void (source = controller),
        })
      )
      const progress: { count: number; files: number }[] = []
      const saved: string[] = []
      const outcome = exportLogFiles({
        filters: {},
        range: { start: '', end: '' },
        type: 'json',
        headers: [],
        locale: 'en',
        signal: new AbortController().signal,
        save: (_blob, name) => saved.push(name),
        onProgress: (value) => progress.push({ ...value }),
        volumeBytes: 600,
      })
      source.enqueue(encoder.encode(rows(3).join('\n') + '\n'))
      // vi.waitFor would advance the faked frames; poll with real timers.
      while (saved.length === 0)
        await new Promise((resolve) => setTimeout(resolve, 1))
      expect(progress).toEqual([])
      vi.advanceTimersToNextFrame()
      expect(progress).toEqual([{ count: 3, files: saved.length }])
      vi.advanceTimersToNextFrame()
      expect(progress).toHaveLength(1)
      source.enqueue(encoder.encode(success(3)))
      source.close()
      await outcome
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('log export filenames', () => {
  const today = new Date(2026, 6, 27, 23, 30)
  it.each([
    ['2026-07-01', '2026-07-31', 'ren2hub-logs-20260701-20260731'],
    ['2026-07-01', '', 'ren2hub-logs-20260701-20260727'],
    ['', '2026-07-31', 'ren2hub-logs-all-20260731'],
    ['', '', 'ren2hub-logs-all-20260727'],
  ])('names %j to %j after the local dates', (start, end, base) => {
    expect(logExportFilename({ start, end }, 'csv', null, today)).toBe(
      `${base}.csv`
    )
    expect(logExportFilename({ start, end }, 'xls', 2, today)).toBe(
      `${base}-part2.xls`
    )
  })

  it('uses the local calendar day, not the UTC one', () => {
    // 00:30 local on 1 August is still 31 July in UTC for zones east of UTC.
    expect(
      logExportFilename(
        { start: '', end: '' },
        'json',
        null,
        new Date(2026, 7, 1, 0, 30)
      )
    ).toBe('ren2hub-logs-all-20260801.json')
  })
})
