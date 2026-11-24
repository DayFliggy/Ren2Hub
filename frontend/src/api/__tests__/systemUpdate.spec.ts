import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import { publicApi } from '../public'
import { ApiError } from '../types'
import {
  LONG_OPERATION_TIMEOUT_MS,
  RESTART_DEADLINE_MS,
  systemUpdateApi,
  watchServerRestart,
  type RestartExpectation,
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
  it('calls the five endpoints with the contract paths, methods and bodies', async () => {
    const signal = new AbortController().signal
    const get = vi
      .spyOn(api, 'get')
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

    await systemUpdateApi.checkUpdates(true, signal)
    await systemUpdateApi.rollbackVersions(signal)
    await systemUpdateApi.update()
    await systemUpdateApi.rollback('1.0.0')
    await systemUpdateApi.restart()

    expect(get.mock.calls).toEqual([
      ['/api/admin/system/check-updates', { force: true }, expect.anything()],
      ['/api/admin/system/rollback-versions', undefined, expect.anything()],
    ])
    expect(get.mock.calls[0]![2]).toMatchObject({ signal })
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
    // The backend gives up after 5 minutes and Nginx after 360 seconds; the
    // browser waits for the backend answer but not past the proxy.
    expect(LONG_OPERATION_TIMEOUT_MS).toBe(330_000)
  })

  it('parses check, rollback list and action results', async () => {
    vi.spyOn(api, 'get')
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
    get.mockResolvedValueOnce({
      current_version: 'v1.0.1',
      latest_version: 'v1.0.2',
      has_update: true,
      build_type: 'docker',
      cached: false,
    })
    await expect(systemUpdateApi.checkUpdates(false)).rejects.toMatchObject(
      invalid
    )
    get.mockResolvedValueOnce({
      current_version: '',
      latest_version: 'v1.0.2',
      has_update: true,
      build_type: 'release',
      cached: false,
    })
    await expect(systemUpdateApi.checkUpdates(false)).rejects.toMatchObject(
      invalid
    )
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
  const baseline = { version: 'v1.0.1' }
  const old = { version: 'v1.0.1', start_time: 100 }
  const watch = (
    expectation: RestartExpectation,
    signal = new AbortController().signal
  ) =>
    watchServerRestart(
      baseline,
      expectation,
      Date.now() + RESTART_DEADLINE_MS,
      signal
    )

  it('after an update, succeeds on any version other than the one before the restart', async () => {
    vi.useFakeTimers()
    // A release newer than the one the check reported was installed.
    const status = vi
      .spyOn(publicApi, 'status')
      .mockResolvedValueOnce(old)
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 160 })
      .mockResolvedValueOnce({ version: 'v1.0.3', start_time: 170 })
    const outcome = watch({ kind: 'new-version' })
    await vi.advanceTimersByTimeAsync(10_000)
    await expect(outcome).resolves.toBe('restarted')
    // A new process still running the old binary is not success.
    expect(status).toHaveBeenCalledTimes(3)
  })

  it('after a rollback, succeeds only on the rollback target', async () => {
    vi.useFakeTimers()
    const status = vi
      .spyOn(publicApi, 'status')
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 160 })
      .mockResolvedValueOnce({ version: 'v1.0.2', start_time: 170 })
      .mockResolvedValueOnce({ version: 'v1.0.0', start_time: 180 })
    const outcome = watch({ kind: 'version', version: '1.0.0' })
    await vi.advanceTimersByTimeAsync(10_000)
    await expect(outcome).resolves.toBe('restarted')
    expect(status).toHaveBeenCalledTimes(3)
  })

  it('keeps polling through failed reads and reports unknown after 120 seconds', async () => {
    vi.useFakeTimers()
    const status = vi
      .spyOn(publicApi, 'status')
      .mockRejectedValueOnce(new ApiError('Bad Gateway', { status: 502 }))
      .mockRejectedValueOnce(new Error('Network Error'))
      .mockResolvedValue(old)
    let settled = ''
    const outcome = watch({ kind: 'new-version' }).then(
      (value) => (settled = value)
    )
    await vi.advanceTimersByTimeAsync(119_000)
    expect(settled).toBe('')
    expect(status.mock.calls.length).toBeGreaterThan(3)
    await vi.advanceTimersByTimeAsync(1_000)
    await outcome
    expect(settled).toBe('unknown')
  })

  it('reports unknown when a read sent before the deadline answers success after it', async () => {
    vi.useFakeTimers()
    const start = Date.now()
    vi.spyOn(publicApi, 'status').mockImplementation(() =>
      Date.now() - start < 118_000
        ? Promise.resolve(old)
        : new Promise((resolve) =>
            setTimeout(
              () => resolve({ version: 'v1.0.2', start_time: 200 }),
              5_000
            )
          )
    )
    const outcome = watch({ kind: 'new-version' })
    await vi.advanceTimersByTimeAsync(130_000)
    await expect(outcome).resolves.toBe('unknown')
  })

  it('limits each read to the time left before the deadline', async () => {
    vi.useFakeTimers()
    const start = Date.now()
    const signals: AbortSignal[] = []
    vi.spyOn(publicApi, 'status').mockImplementation((signal) => {
      signals.push(signal!)
      if (Date.now() - start < 118_000) return Promise.resolve(old)
      return new Promise((_resolve, reject) =>
        signal!.addEventListener('abort', () => reject(new Error('aborted')))
      )
    })
    let settled = ''
    const outcome = watch({ kind: 'new-version' }).then(
      (value) => (settled = value)
    )
    await vi.advanceTimersByTimeAsync(119_500)
    expect(signals.at(-1)!.aborted).toBe(false)
    await vi.advanceTimersByTimeAsync(500)
    await outcome
    expect(signals.at(-1)!.aborted).toBe(true)
    expect(settled).toBe('unknown')
    expect(Date.now() - start).toBe(120_000)
  })

  it('stops when aborted', async () => {
    vi.useFakeTimers()
    const status = vi.spyOn(publicApi, 'status').mockResolvedValue(old)
    const controller = new AbortController()
    const outcome = watch({ kind: 'new-version' }, controller.signal)
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

  it('ignores a success that arrives after the watch is aborted', async () => {
    vi.useFakeTimers()
    type Status = Awaited<ReturnType<typeof publicApi.status>>
    let answer: (value: Status) => void = () => {}
    vi.spyOn(publicApi, 'status').mockImplementation(
      () => new Promise((resolve) => (answer = resolve))
    )
    const controller = new AbortController()
    const outcome = watch({ kind: 'new-version' }, controller.signal)
    const rejected = expect(outcome).rejects.toMatchObject({
      name: 'AbortError',
    })
    await vi.advanceTimersByTimeAsync(2_000)
    controller.abort()
    // A transport that already finished hands the answer over regardless.
    answer({ version: 'v1.0.2', start_time: 200 })
    await rejected
  })
})
