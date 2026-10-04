import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/client'
import { ApiError } from '@/api/types'

import {
  deleteTaskPluginVersion,
  getTaskPluginEnabledOption,
  setTaskPluginEnabledOption,
  setTaskPluginStatus,
  TaskPluginUsageError,
  updateMarketplaceSources,
} from '../api'
import {
  fetchPluginSourceText,
  normalizePluginSourceUrl,
  PluginSourceFetchError,
} from '../lib/plugin-url'

afterEach(() => vi.restoreAllMocks())

describe('task plugin API contract', () => {
  it('sends disable options only for a disable', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(null)
    await setTaskPluginStatus('fixture/key', false, {
      cascade: true,
      force: true,
    })
    expect(post).toHaveBeenLastCalledWith(
      '/api/plugin/task/fixture%2Fkey/status?cascade=true&force=true',
      { enabled: false }
    )
    await setTaskPluginStatus('fixture/key', true, {
      cascade: true,
      force: true,
    })
    expect(post).toHaveBeenLastCalledWith(
      '/api/plugin/task/fixture%2Fkey/status',
      { enabled: true }
    )
  })

  it('turns only a valid in-use payload into a usage error', async () => {
    const usage = {
      channels: [{ id: 7, name: 'Gateway', type: 60 }],
      in_flight_count: 2,
    }
    vi.spyOn(api, 'post').mockRejectedValue(
      new ApiError('in use', { business: true, data: usage })
    )
    await expect(setTaskPluginStatus('fixture', false)).rejects.toBeInstanceOf(
      TaskPluginUsageError
    )
    await expect(setTaskPluginStatus('fixture', true)).rejects.toBeInstanceOf(
      ApiError
    )
    for (const data of [
      { channels: [], in_flight_count: -1 },
      { channels: [{ id: 7, name: 'Gateway' }], in_flight_count: 0 },
    ]) {
      const error = new ApiError('bad usage', { business: true, data })
      vi.spyOn(api, 'delete').mockRejectedValueOnce(error)
      await expect(deleteTaskPluginVersion('fixture', '1')).rejects.toBe(error)
    }
  })

  it('reads and writes the TaskPluginEnabled master switch option', async () => {
    vi.spyOn(api, 'get').mockResolvedValue([
      { key: 'TaskEnabled', value: 'true' },
      { key: 'TaskPluginEnabled', value: 'false' },
    ])
    await expect(getTaskPluginEnabledOption()).resolves.toBe(false)
    const put = vi.spyOn(api, 'put').mockResolvedValue(null)
    await setTaskPluginEnabledOption(true)
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'TaskPluginEnabled',
      value: 'true',
    })
    put.mockResolvedValueOnce(null)
    await expect(updateMarketplaceSources([])).resolves.toEqual([])
  })
})

describe('plugin URL import', () => {
  it('accepts HTTPS only and rewrites GitHub and gist pages to raw URLs', () => {
    expect(normalizePluginSourceUrl('http://example.com/plugin.js')).toBeNull()
    expect(normalizePluginSourceUrl('javascript:alert(1)')).toBeNull()
    expect(
      normalizePluginSourceUrl(
        'https://github.com/owner/repo/blob/main/tasks/plugin.js#L3'
      )
    ).toBe('https://raw.githubusercontent.com/owner/repo/main/tasks/plugin.js')
    expect(normalizePluginSourceUrl('https://gist.github.com/user/abc')).toBe(
      'https://gist.githubusercontent.com/user/abc/raw'
    )
  })

  it('fetches without credentials or redirects and stops at the size cap', async () => {
    const fetcher = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(new Uint8Array(8 * 1024 * 1024 + 1)))
    await expect(
      fetchPluginSourceText('https://plugins.example/plugin.js')
    ).rejects.toMatchObject({ reason: 'too_large' })
    expect(fetcher).toHaveBeenCalledWith(
      'https://plugins.example/plugin.js',
      expect.objectContaining({
        credentials: 'omit',
        redirect: 'error',
        cache: 'no-cache',
        referrerPolicy: 'no-referrer',
      })
    )
    fetcher.mockResolvedValueOnce(new Response('', { status: 404 }))
    const error = await fetchPluginSourceText(
      'https://plugins.example/missing.js'
    ).catch((cause: unknown) => cause)
    expect(error).toBeInstanceOf(PluginSourceFetchError)
    expect(error).toMatchObject({ reason: 'not_found', status: 404 })
  })
})
