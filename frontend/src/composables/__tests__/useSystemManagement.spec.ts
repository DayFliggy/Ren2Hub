import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseSystemTask, systemManagementApi } from '@/api/systemManagement'
import { useSystemManagement } from '../useSystemManagement'

const running = parseSystemTask({
  task_id: 'task',
  type: 'log_cleanup',
  status: 'running',
  created_at: 1,
  updated_at: 2,
})
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
describe('native system task lifecycles', () => {
  it('derives type suggestions from real loaded records only', async () => {
    vi.mocked(systemManagementApi.tasks).mockImplementation(async (query) => ({
      items:
        query.scope === 'active'
          ? [running]
          : [{ ...running, type: 'channel_test', status: 'succeeded' }],
      total: 1,
    }))
    wrapper = mount(Host)
    await flushPromises()
    expect(state.taskTypes.value).toEqual(['channel_test', 'log_cleanup'])
  })
  it('polls running tasks quickly and discovers new work while idle', async () => {
    wrapper = mount(Host)
    await flushPromises()
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(2)
    vi.mocked(systemManagementApi.tasks).mockImplementation(async (query) => ({
      items: query.scope === 'active' ? [running] : [],
      total: query.scope === 'active' ? 1 : 0,
    }))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(state.activeTasks.value).toEqual([running])
    const before = vi
      .mocked(systemManagementApi.tasks)
      .mock.calls.filter(([q]) => q.scope === 'active').length
    await vi.advanceTimersByTimeAsync(8000)
    expect(
      vi
        .mocked(systemManagementApi.tasks)
        .mock.calls.filter(([q]) => q.scope === 'active')
    ).toHaveLength(before + 1)
  })
  it.each(['succeeded', 'failed'] as const)(
    'refreshes history for a short task that %s between active polls',
    async (status) => {
      wrapper = mount(Host)
      await flushPromises()
      const completed = { ...running, status }
      vi.mocked(systemManagementApi.tasks).mockImplementation(
        async (query) => ({
          items: query.scope === 'history' ? [completed] : [],
          total: query.scope === 'history' ? 1 : 0,
        })
      )
      await vi.advanceTimersByTimeAsync(60_000)
      expect(state.historyTasks.value).toEqual([completed])
      expect(state.historyTotal.value).toBe(1)
    }
  )
  it('uses native pagination and resets offsets when filters change', async () => {
    wrapper = mount(Host)
    await flushPromises()
    state.historyPage.value = 3
    await flushPromises()
    expect(systemManagementApi.tasks).toHaveBeenLastCalledWith(
      { scope: 'history', type: '', status: '', offset: 40, limit: 20 },
      expect.any(AbortSignal)
    )
    state.historyType.value = 'channel_test'
    state.historyStatus.value = 'failed'
    await flushPromises()
    expect(state.historyPage.value).toBe(1)
    expect(systemManagementApi.tasks).toHaveBeenLastCalledWith(
      {
        scope: 'history',
        type: 'channel_test',
        status: 'failed',
        offset: 0,
        limit: 20,
      },
      expect.any(AbortSignal)
    )
  })
  it('shows native list failures and stops requests on unmount', async () => {
    vi.mocked(systemManagementApi.tasks).mockRejectedValue(
      new Error('Task storage unavailable')
    )
    wrapper = mount(Host)
    await flushPromises()
    expect(state.active.error.value).toBe('Task storage unavailable')
    expect(state.history.error.value).toBe('Task storage unavailable')
    wrapper.unmount()
    wrapper = undefined
    const before = vi.mocked(systemManagementApi.tasks).mock.calls.length
    await vi.advanceTimersByTimeAsync(60_000)
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(before)
  })
})
