import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { exportUserLogs, parseLogExport } from '@/api/logExport'

const row = {
  id: 1,
  type: 2,
  token_name: 'fixture',
  model_name: 'model',
  channel_name: 'channel',
  quota: '9007199254740993',
  prompt_tokens: 1,
  completion_tokens: 2,
  use_time: 1000,
  is_stream: true,
  content: '=untrusted',
  created_at: 1700000000,
}
const terminal = { success: true, data: { count: 1 } }
const stream = `${JSON.stringify(row)}\n${JSON.stringify(terminal)}`

afterEach(() => vi.restoreAllMocks())

describe('usage log export', () => {
  it('requires a successful terminal record matching the complete row count', () => {
    expect(parseLogExport(stream)[0]).toMatchObject({
      quota: '9007199254740993',
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
    controller.abort()
    await expect(exportUserLogs({}, controller.signal)).rejects.toMatchObject({
      name: 'AbortError',
    })
  })
})
