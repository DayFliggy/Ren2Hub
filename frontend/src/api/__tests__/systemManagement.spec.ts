import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  parseSystemTask,
  parseSystemTaskSummary,
  systemManagementApi,
} from '../systemManagement'
import { summaryFixture } from './opsFixtures'

const task = {
  task_id: 'task-1',
  type: 'billing_recovery',
  status: 'running',
  created_at: 1,
  updated_at: 2,
}
afterEach(() => vi.restoreAllMocks())

describe('system management response contracts', () => {
  it('lists registered task types with unrecorded runs left as null', () => {
    const [channelTest, cleanup] = parseSystemTaskSummary(summaryFixture())
    expect(channelTest).toMatchObject({
      type: 'channel_test',
      interval_seconds: 600,
      last_failure_at: null,
      status: 'ok',
    })
    expect(cleanup).toMatchObject({
      type: 'gateway_state_cleanup',
      last_success_at: null,
      last_error: 'lock lost',
      status: 'failing',
    })
  })

  it('rejects summaries with unknown health or malformed handler fields', () => {
    const [row] = summaryFixture().types
    for (const invalid of [
      { ...row, status: 'healthy' },
      { ...row, type: '' },
      { ...row, active: 'false' },
      { ...row, last_success_at: '1767225000' },
    ])
      expect(() => parseSystemTaskSummary({ types: [invalid] })).toThrow(
        'Invalid API response'
      )
    expect(() => parseSystemTaskSummary([row])).toThrow()
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
            { source: 'models_dev', reason: 'No catalog entry' },
            { source: 'openrouter', reason: 'Source unavailable' },
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
