import { describe, it, expect, vi } from 'vitest'
import {
  marketSourceURL,
  parseMarketIndex,
  readMarketText,
  taskPluginsApi,
} from '../taskPlugins'
import { api } from '../client'
import { ApiError } from '../types'
import { TaskPluginUsageError } from '../taskPlugins'

describe('task plugin marketplace integrity', () => {
  const source = {
    name: 'fixture',
    index_url: 'https://plugins.example/index.json',
  }
  it('requires a same-origin HTTPS source and a hash for every installable version', () => {
    expect(marketSourceURL(source.index_url, 'test/plugin.js')).toBe(
      'https://plugins.example/test/plugin.js'
    )
    expect(() =>
      marketSourceURL(source.index_url, '//other.example/a.js')
    ).toThrow()
    expect(() =>
      marketSourceURL('http://plugins.example/index.json', 'a.js')
    ).toThrow()
    const plugins = parseMarketIndex(
      {
        indexVersion: 1,
        plugins: [
          {
            key: 'good',
            versions: [{ version: '1', path: 'a.js', sha256: 'a'.repeat(64) }],
          },
          { key: 'unverified', versions: [{ version: '1', path: 'b.js' }] },
        ],
      },
      source
    )
    expect(plugins.map((plugin) => plugin.key)).toEqual(['good'])
  })
  it('bounds untrusted downloads and sends no browser credentials', async () => {
    const fetcher = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('oversized'))
    await expect(
      readMarketText('https://plugins.example/a.js', 2)
    ).rejects.toThrow('size limit')
    expect(fetcher).toHaveBeenCalledWith(
      'https://plugins.example/a.js',
      expect.objectContaining({ credentials: 'omit', redirect: 'error' })
    )
    fetcher.mockRestore()
  })
  it('sends marketplace sources in the backend array contract', async () => {
    const put = vi.spyOn(api, 'put').mockResolvedValue(undefined)
    await taskPluginsApi.saveSources([source])
    expect(put).toHaveBeenCalledWith('/api/plugin/task/marketplace/sources', [
      source,
    ])
    put.mockRestore()
  })
  it('sends explicitly requested disable options and never adds them to enable or delete', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(undefined)
    const remove = vi.spyOn(api, 'delete').mockResolvedValue(undefined)
    await taskPluginsApi.enabled('fixture/key', false, {
      cascade: true,
      force: true,
    })
    expect(post).toHaveBeenLastCalledWith(
      '/api/plugin/task/fixture%2Fkey/status?cascade=true&force=true',
      { enabled: false }
    )
    await taskPluginsApi.enabled('fixture/key', true, {
      cascade: true,
      force: true,
    })
    expect(post).toHaveBeenLastCalledWith(
      '/api/plugin/task/fixture%2Fkey/status',
      { enabled: true }
    )
    await taskPluginsApi.remove('fixture/key', '1.0')
    expect(remove).toHaveBeenCalledWith(
      '/api/plugin/task/fixture%2Fkey/versions/1.0'
    )
  })
  it.each([
    { channels: [], in_flight_count: -1 },
    { channels: [{ id: 7, name: 'Channel' }], in_flight_count: 0 },
    { channels: [{ id: 7, name: 'Channel', type: 60 }], in_flight_count: '2' },
  ])(
    'keeps an invalid usage payload as an ordinary error: %j',
    async (data) => {
      const error = new ApiError('Invalid usage', { business: true, data })
      vi.spyOn(api, 'post').mockRejectedValue(error)
      await expect(taskPluginsApi.enabled('fixture', false)).rejects.toBe(error)
    }
  )
  it('preserves valid usage only for a disable business error', async () => {
    const usage = {
      channels: [{ id: 7, name: 'Channel', type: 60 }],
      in_flight_count: 2,
    }
    vi.spyOn(api, 'post').mockRejectedValue(
      new ApiError('in use', { business: true, data: usage })
    )
    await expect(
      taskPluginsApi.enabled('fixture', false)
    ).rejects.toBeInstanceOf(TaskPluginUsageError)
    await expect(
      taskPluginsApi.enabled('fixture', true)
    ).rejects.toBeInstanceOf(ApiError)
  })
})
