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

  it('uses request signals and requires the real deleted count', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValue([task] as never)
    await systemManagementApi.tasks(20, signal)
    expect(get).toHaveBeenCalledWith(
      '/api/system-task/list',
      { limit: 20 },
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
