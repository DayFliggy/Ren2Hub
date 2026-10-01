import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  parseSystemTask,
  parseSystemTaskSummary,
  systemManagementApi,
} from '@/api/systemManagement'
import { summaryFixture } from '@/api/__tests__/opsFixtures'
import { useSystemManagement } from '../useSystemManagement'

const running = parseSystemTask({
  task_id: 'task',
  type: 'log_cleanup',
  status: 'running',
  created_at: 1,
  updated_at: 2,
})
const summary = parseSystemTaskSummary(summaryFixture())
let state: ReturnType<typeof useSystemManagement>
let wrapper: VueWrapper | undefined
const Host = defineComponent({
  setup() {
    state = useSystemManagement()
    return () => null
  },
})

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.spyOn(systemManagementApi, 'summary').mockResolvedValue(summary)
  vi.spyOn(systemManagementApi, 'tasks').mockResolvedValue({
    items: [],
    total: 0,
  })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('system task lifecycles', () => {
  it('offers the registered task types from the summary as history filters', async () => {
    wrapper = mount(Host)
    await flushPromises()
    expect(state.taskTypes.value).toEqual([
      'channel_test',
      'gateway_state_cleanup',
    ])
  })

  it('polls active tasks every 8 seconds only while one is active', async () => {
    vi.mocked(systemManagementApi.tasks).mockImplementation(async (query) => ({
      items: query.scope === 'active' ? [running] : [],
      total: query.scope === 'active' ? 1 : 0,
    }))
    wrapper = mount(Host)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(8000)
    // The active poll plus the history refresh it triggers.
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(4)
    vi.mocked(systemManagementApi.tasks).mockImplementation(async (query) => ({
      items:
        query.scope === 'history' ? [{ ...running, status: 'succeeded' }] : [],
      total: query.scope === 'history' ? 1 : 0,
    }))
    await vi.advanceTimersByTimeAsync(8000)
    expect(state.activeTasks.value).toEqual([])
    expect(state.historyTasks.value).toHaveLength(1)
    const settled = vi.mocked(systemManagementApi.tasks).mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(settled)
  })

  it('restarts active polling when the summary reports a newly started run', async () => {
    wrapper = mount(Host)
    await flushPromises()
    const before = vi.mocked(systemManagementApi.tasks).mock.calls.length
    vi.mocked(systemManagementApi.summary).mockResolvedValue(
      summary.map((row, i) => (i === 0 ? { ...row, active: true } : row))
    )
    await vi.advanceTimersByTimeAsync(60_000)
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(before + 1)
    expect(systemManagementApi.tasks).toHaveBeenLastCalledWith(
      { scope: 'active', limit: 100 },
      expect.any(AbortSignal)
    )
  })

  it('uses server pagination and resets the offset when filters change', async () => {
    wrapper = mount(Host)
    await flushPromises()
    state.historyPage.value = 3
    await flushPromises()
    expect(systemManagementApi.tasks).toHaveBeenLastCalledWith(
      { scope: 'history', type: '', status: '', offset: 40, limit: 20 },
      expect.any(AbortSignal)
    )
    state.historyType.value = 'log_cleanup'
    state.historyStatus.value = 'failed'
    await flushPromises()
    expect(state.historyPage.value).toBe(1)
    expect(systemManagementApi.tasks).toHaveBeenLastCalledWith(
      {
        scope: 'history',
        type: 'log_cleanup',
        status: 'failed',
        offset: 0,
        limit: 20,
      },
      expect.any(AbortSignal)
    )
  })
})
