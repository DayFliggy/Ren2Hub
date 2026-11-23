import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import { publicApi } from '../public'
import { ApiError } from '../types'
import {
  LONG_OPERATION_TIMEOUT_MS,
  systemUpdateApi,
  watchServerRestart,
} from '../systemUpdate'

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

const release = {
  name: 'Ren2Hub v1.0.2',
  body: '## Fixes\n- one',
  published_at: '2026-11-20T08:00:00Z',
  html_url: 'https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.2',
}

describe('system update API contract', () => {
  it('calls the six endpoints with the contract paths, methods and bodies', async () => {
    const signal = new AbortController().signal
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce({ version: 'v1.0.1', build_type: 'release' })
      .mockResolvedValueOnce({
        current_version: 'v1.0.1',
        latest_version: 'v1.0.1',
        has_update: false,
        build_type: 'release',
        cached: true,
      })
      .mockResolvedValueOnce({ versions: [] })
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce({ need_restart: true })
      .mockResolvedValueOnce({ need_restart: true, version: '1.0.0' })
      .mockResolvedValueOnce({ restarting: true })

    await systemUpdateApi.version(signal)
    await systemUpdateApi.checkUpdates(true, signal)
    await systemUpdateApi.rollbackVersions(signal)
    await systemUpdateApi.update()
    await systemUpdateApi.rollback('1.0.0')
    await systemUpdateApi.restart()

    expect(get.mock.calls).toEqual([
      ['/api/admin/system/version', undefined, { signal }],
      ['/api/admin/system/check-updates', { force: true }, expect.anything()],
      ['/api/admin/system/rollback-versions', undefined, expect.anything()],
    ])
    expect(get.mock.calls[1]![2]).toMatchObject({ signal })
    expect(post.mock.calls).toEqual([
      [
        '/api/admin/system/update',
        undefined,
        { timeoutMs: LONG_OPERATION_TIMEOUT_MS },
      ],
      [
        '/api/admin/system/rollback',
        { version: '1.0.0' },
        { timeoutMs: LONG_OPERATION_TIMEOUT_MS },
      ],
      ['/api/admin/system/restart', undefined, undefined],
    ])
    // The backend may spend up to 15 minutes downloading and replacing.
    expect(LONG_OPERATION_TIMEOUT_MS).toBeGreaterThan(15 * 60_000)
  })

  it('parses version, check, rollback list and action results', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce({ version: 'v1.0.1', build_type: 'source' })
      .mockResolvedValueOnce({
        current_version: 'v1.0.1',
        latest_version: 'v1.0.2',
        has_update: true,
        build_type: 'release',
        cached: true,
        warning: 'GitHub unreachable, using cache',
        release_info: release,
      })
      .mockResolvedValueOnce({
        current_version: 'v1.0.2',
        latest_version: 'v1.0.2',
        has_update: false,
        build_type: 'release',
        cached: false,
      })
      .mockResolvedValueOnce({
        versions: [
          {
            version: '1.0.1',
            published_at: '2026-11-10T00:00:00Z',
            html_url: release.html_url,
          },
        ],
      })
    vi.spyOn(api, 'post')
      .mockResolvedValueOnce({ need_restart: true })
      .mockResolvedValueOnce({
        already_up_to_date: true,
        current_version: 'v1.0.2',
        latest_version: 'v1.0.2',
      })
      .mockResolvedValueOnce({ need_restart: true, version: '1.0.1' })
      .mockResolvedValueOnce({ restarting: true })

    expect(await systemUpdateApi.version()).toEqual({
      version: 'v1.0.1',
      build_type: 'source',
    })
    expect(await systemUpdateApi.checkUpdates(false)).toEqual({
      current_version: 'v1.0.1',
      latest_version: 'v1.0.2',
      has_update: true,
      build_type: 'release',
      cached: true,
      warning: true,
      release_info: release,
    })
    expect(await systemUpdateApi.checkUpdates(false)).toMatchObject({
      warning: false,
      release_info: null,
    })
    expect(await systemUpdateApi.rollbackVersions()).toEqual([
      {
        version: '1.0.1',
        published_at: '2026-11-10T00:00:00Z',
        html_url: release.html_url,
      },
    ])
    expect(await systemUpdateApi.update()).toEqual({ kind: 'restart' })
    expect(await systemUpdateApi.update()).toEqual({
      kind: 'current',
      current_version: 'v1.0.2',
      latest_version: 'v1.0.2',
    })
    expect(await systemUpdateApi.rollback('1.0.1')).toEqual({
      version: '1.0.1',
    })
    await expect(systemUpdateApi.restart()).resolves.toBeUndefined()
  })

  it('rejects malformed responses', async () => {
    const invalid = { status: 502, code: 'INVALID_RESPONSE' }
    const get = vi.spyOn(api, 'get')
    const post = vi.spyOn(api, 'post')
    get.mockResolvedValueOnce({ version: 'v1.0.1', build_type: 'docker' })
    await expect(systemUpdateApi.version()).rejects.toMatchObject(invalid)
    get.mockResolvedValueOnce({ version: '', build_type: 'release' })
    await expect(systemUpdateApi.version()).rejects.toMatchObject(invalid)
    get.mockResolvedValueOnce({
      current_version: 'v1.0.1',
      latest_version: 'v1.0.2',
      has_update: 'yes',
      build_type: 'release',
      cached: false,
    })
    await expect(systemUpdateApi.checkUpdates(false)).rejects.toMatchObject(
      invalid
    )
    get.mockResolvedValueOnce({
      current_version: 'v1.0.1',
      latest_version: 'v1.0.2',
      has_update: true,
      build_type: 'release',
      cached: false,
      release_info: { ...release, html_url: 'javascript:alert(1)' },
    })
    await expect(systemUpdateApi.checkUpdates(false)).rejects.toMatchObject(
      invalid
    )
    get.mockResolvedValueOnce({ versions: [{ version: 101 }] })
    await expect(systemUpdateApi.rollbackVersions()).rejects.toMatchObject(
      invalid
    )
    post.mockResolvedValueOnce({ need_restart: false })
    await expect(systemUpdateApi.update()).rejects.toMatchObject(invalid)
    post.mockResolvedValueOnce({ need_restart: true })
    await expect(systemUpdateApi.rollback('1.0.1')).rejects.toMatchObject(
      invalid
    )
    post.mockResolvedValueOnce({})
    await expect(systemUpdateApi.restart()).rejects.toMatchObject(invalid)
  })
})

describe('server restart watch', () => {
  const baseline = { version: 'v1.0.1', start_time: 100 }

  it('succeeds once the target version answers', async () => {
    vi.useFakeTimers()
    const status = vi
      .spyOn(publicApi, 'status')
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 160 })
      .mockResolvedValueOnce({ version: 'v1.0.2', start_time: 170 })
    const outcome = watchServerRestart(
      baseline,
      '1.0.2',
      new AbortController().signal
    )
    await vi.advanceTimersByTimeAsync(10_000)
    await expect(outcome).resolves.toBe('restarted')
    // A new process with the old binary is not the release that was asked for.
    expect(status).toHaveBeenCalledTimes(3)
  })

  it('succeeds on a new start time when no target version is expected', async () => {
    vi.useFakeTimers()
    vi.spyOn(publicApi, 'status')
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 180 })
    const outcome = watchServerRestart(
      baseline,
      null,
      new AbortController().signal
    )
    await vi.advanceTimersByTimeAsync(10_000)
    await expect(outcome).resolves.toBe('restarted')
  })

  it('keeps polling through failed reads and reports unknown after 120 seconds', async () => {
    vi.useFakeTimers()
    const status = vi
      .spyOn(publicApi, 'status')
      .mockRejectedValueOnce(new ApiError('Bad Gateway', { status: 502 }))
      .mockRejectedValueOnce(new Error('Network Error'))
      .mockResolvedValue({ version: 'v1.0.1', start_time: 100 })
    let settled = ''
    const outcome = watchServerRestart(
      baseline,
      'v1.0.2',
      new AbortController().signal
    ).then((value) => (settled = value))
    await vi.advanceTimersByTimeAsync(119_000)
    expect(settled).toBe('')
    expect(status.mock.calls.length).toBeGreaterThan(3)
    await vi.advanceTimersByTimeAsync(5_000)
    await outcome
    expect(settled).toBe('unknown')
  })

  it('stops when aborted', async () => {
    vi.useFakeTimers()
    const status = vi
      .spyOn(publicApi, 'status')
      .mockResolvedValue({ version: 'v1.0.1', start_time: 100 })
    const controller = new AbortController()
    const outcome = watchServerRestart(baseline, null, controller.signal)
    const rejected = expect(outcome).rejects.toMatchObject({
      name: 'AbortError',
    })
    await vi.advanceTimersByTimeAsync(5_000)
    const calls = status.mock.calls.length
    controller.abort()
    await rejected
    await vi.advanceTimersByTimeAsync(30_000)
    expect(status).toHaveBeenCalledTimes(calls)
  })
})
