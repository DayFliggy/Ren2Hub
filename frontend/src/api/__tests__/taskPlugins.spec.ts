import { describe, it, expect, vi } from 'vitest'
import {
  marketSourceURL,
  parseMarketIndex,
  readMarketText,
  taskPluginsApi,
} from '../taskPlugins'
import { api } from '../client'

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
})
