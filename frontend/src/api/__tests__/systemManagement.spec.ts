import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import { parseSystemTask, systemManagementApi } from '../systemManagement'

const task = {
  task_id: 'task-1',
  type: 'channel_test',
  status: 'running',
  created_at: 1,
  updated_at: 2,
}
afterEach(() => vi.restoreAllMocks())

describe('system management response contracts', () => {
  it('uses native current/detail responses and preserves a missing current task', async () => {
    const signal = new AbortController().signal
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce(task)
      .mockResolvedValueOnce(task)
    expect(await systemManagementApi.current('channel_test', signal)).toBeNull()
    expect(get).toHaveBeenNthCalledWith(
      1,
      '/api/system-task/current',
      { type: 'channel_test' },
      { signal }
    )
    expect(
      await systemManagementApi.current('channel_test', signal)
    ).toMatchObject({ task_id: 'task-1' })
    expect(await systemManagementApi.task('task/one', signal)).toMatchObject({
      task_id: 'task-1',
    })
    expect(get).toHaveBeenLastCalledWith(
      '/api/system-task/task%2Fone',
      undefined,
      { signal }
    )
    get.mockRejectedValueOnce(new Error('task not found'))
    await expect(systemManagementApi.task('missing')).rejects.toThrow(
      'task not found'
    )
  })
  it('rejects malformed task statuses and fields', () => {
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
    const remove = vi.spyOn(api, 'delete').mockResolvedValueOnce({} as never)
    await expect(
      systemManagementApi.deleteHistory({ type: '', status: '' })
    ).rejects.toThrow()
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
