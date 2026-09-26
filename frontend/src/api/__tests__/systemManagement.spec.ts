import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  parseSystemInstance,
  parseSystemTask,
  systemManagementApi,
} from '../systemManagement'

const instance = {
  node_name: 'node-1',
  status: 'online',
  started_at: 1,
  last_seen_at: 2,
}
const task = {
  task_id: 'task-1',
  type: 'billing_recovery',
  status: 'running',
  created_at: 1,
  updated_at: 2,
}
afterEach(() => vi.restoreAllMocks())

describe('system management response contracts', () => {
  it('preserves absent metrics and role as unknown instead of inventing zero or worker values', () => {
    expect(parseSystemInstance(instance)).toMatchObject({
      cpu: null,
      memory: null,
      storage: null,
      storage_total: null,
      master: null,
      manually_configured: null,
    })
    expect(
      parseSystemInstance({
        ...instance,
        info: {
          role: { is_master: false },
          resources: { cpu: { usage_percent: 0 }, storage: { total_bytes: 0 } },
        },
      })
    ).toMatchObject({ cpu: 0, storage_total: 0, master: false })
  })

  it('retains node identity, platform and complete storage details from the backend', () => {
    expect(
      parseSystemInstance({
        ...instance,
        stale_after_seconds: 90,
        info: {
          node: {
            name: 'primary',
            source: 'manual',
            manually_configured: true,
            should_configure_manually: false,
          },
          role: { is_master: true },
          runtime: { goos: 'linux', goarch: 'amd64', version: 'v1' },
          host: { hostname: 'host' },
          resources: {
            cpu: { usage_percent: 12.5 },
            memory: { usage_percent: 20 },
            storage: {
              total_bytes: 1024,
              used_bytes: 256,
              free_bytes: 768,
              used_percent: 25,
            },
          },
        },
      })
    ).toMatchObject({
      display_name: 'primary',
      name_source: 'manual',
      manually_configured: true,
      should_configure_manually: false,
      master: true,
      goos: 'linux',
      goarch: 'amd64',
      storage_total: 1024,
      storage_used: 256,
      storage_free: 768,
    })
  })

  it('rejects malformed metrics, node flags, statuses and task fields', () => {
    expect(() =>
      parseSystemInstance({
        ...instance,
        info: { resources: { cpu: { usage_percent: '10' } } },
      })
    ).toThrow()
    expect(() =>
      parseSystemInstance({
        ...instance,
        info: { node: { manually_configured: 'false' } },
      })
    ).toThrow()
    expect(() =>
      parseSystemInstance({
        ...instance,
        info: { resources: { storage: { total_bytes: -1 } } },
      })
    ).toThrow()
    expect(() => parseSystemTask({ ...task, status: 'unknown' })).toThrow()
    expect(() =>
      parseSystemTask({ ...task, state: { progress: '20' } })
    ).toThrow()
  })

  it('parses task results without filling missing counters', () => {
    expect(parseSystemTask(task)).toMatchObject({
      progress: null,
      processed: null,
      total: null,
      deleted_count: null,
    })
    expect(
      parseSystemTask({
        ...task,
        state: { progress: 20, processed: 2, total: 10 },
        result: { deleted_count: 2 },
      })
    ).toMatchObject({ progress: 20, processed: 2, total: 10, deleted_count: 2 })
  })

  it('retains bounded automatic pricing outcomes and source reasons without losing zero counts', () => {
    const result = {
      checked: 4,
      changed: 0,
      published: 2,
      deactivated: 1,
      unconfigured_count: 101,
      unconfigured: ['embedding-v1'],
      errors: ['OpenRouter unavailable'],
      unconfigured_details: [
        {
          model: 'embedding-v1',
          reasons: [
            { source: 'sub2api', reason: 'Unsupported input image price' },
          ],
        },
      ],
    }
    const pricing = { ...task, type: 'billing_pricing_sync', result }
    expect(parseSystemTask(pricing).pricing_result).toEqual(result)
    expect(
      parseSystemTask({
        ...pricing,
        result: { ...result, unconfigured_details: undefined },
      }).pricing_result?.unconfigured_details
    ).toEqual([])
    for (const invalid of [
      { ...result, checked: '4' },
      { ...result, published: -1 },
      { ...result, errors: [null] },
      { ...result, unconfigured: new Array(101).fill('model') },
      {
        ...result,
        unconfigured_details: new Array(101).fill(
          result.unconfigured_details[0]
        ),
      },
      {
        ...result,
        unconfigured_details: [
          { model: 'x', reasons: [{ source: 'sub2api', reason: null }] },
        ],
      },
    ])
      expect(() => parseSystemTask({ ...pricing, result: invalid })).toThrow()
  })

  it('loads the latest pricing task across all statuses using the shared list API', async () => {
    const get = vi
      .spyOn(api, 'getEnvelope')
      .mockResolvedValue({ success: true, data: [], total: 0 })
    await systemManagementApi.tasks({ type: 'billing_pricing_sync', limit: 1 })
    expect(get).toHaveBeenCalledWith(
      '/api/system-task/list',
      {
        type: 'billing_pricing_sync',
        limit: 1,
      },
      { signal: undefined }
    )
  })

  it('uses request signals and requires the real deleted count', async () => {
    const signal = new AbortController().signal
    const get = vi
      .spyOn(api, 'getEnvelope')
      .mockResolvedValue({ success: true, data: [task], total: 41 })
    expect(
      await systemManagementApi.tasks(
        {
          scope: 'history',
          type: 'log_cleanup',
          status: 'failed',
          offset: 20,
          limit: 20,
        },
        signal
      )
    ).toMatchObject({ total: 41, items: [{ task_id: 'task-1' }] })
    expect(get).toHaveBeenCalledWith(
      '/api/system-task/list',
      {
        scope: 'history',
        type: 'log_cleanup',
        status: 'failed',
        offset: 20,
        limit: 20,
      },
      { signal }
    )
    const remove = vi
      .spyOn(api, 'delete')
      .mockResolvedValue({ deleted_count: 1 } as never)
    expect(await systemManagementApi.deleteStale('node/one')).toBe(1)
    expect(remove).toHaveBeenCalledWith('/api/system-info/instances/node%2Fone')
    remove.mockRejectedValueOnce(
      new Error('instance is not stale or no longer exists')
    )
    await expect(systemManagementApi.deleteStale('recovered')).rejects.toThrow(
      'not stale'
    )
    remove.mockResolvedValueOnce({} as never)
    await expect(systemManagementApi.deleteStale()).rejects.toThrow()
    remove.mockResolvedValueOnce({ deleted_count: 3 } as never)
    expect(
      await systemManagementApi.deleteHistory({
        type: 'log_cleanup',
        status: 'failed',
      })
    ).toBe(3)
    expect(remove).toHaveBeenLastCalledWith('/api/system-task/history', {
      scope: 'history',
      type: 'log_cleanup',
      status: 'failed',
    })
    get.mockResolvedValueOnce({ success: true, data: [task] })
    await expect(
      systemManagementApi.tasks({ scope: 'history' })
    ).rejects.toThrow()
  })

  it('validates the destructive log cutoff before sending a request', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(task as never)
    await expect(
      systemManagementApi.cleanup(Math.floor(Date.now() / 1000) + 60)
    ).rejects.toThrow()
    await expect(systemManagementApi.cleanup(-1)).rejects.toThrow()
    expect(post).not.toHaveBeenCalled()
    expect(await systemManagementApi.cleanup(1)).toMatchObject({
      task_id: 'task-1',
    })
    expect(post).toHaveBeenCalledWith(
      '/api/system-task/log-cleanup?target_timestamp=1'
    )
  })
})
