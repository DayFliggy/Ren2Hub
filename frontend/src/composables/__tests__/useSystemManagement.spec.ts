import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  parseSystemInstance,
  parseSystemTask,
  systemManagementApi,
  type SystemInstance,
} from '@/api/systemManagement'
import { useSystemManagement } from '../useSystemManagement'

const node = parseSystemInstance({
  node_name: 'node',
  status: 'online',
  started_at: 1,
  last_seen_at: 2,
})
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
  vi.spyOn(systemManagementApi, 'instances').mockResolvedValue([node])
  vi.spyOn(systemManagementApi, 'tasks').mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('system management section lifecycles', () => {
  it('isolates instance, active task and history failures and reloads only the requested section', async () => {
    vi.mocked(systemManagementApi.instances).mockRejectedValueOnce(
      new Error('nodes unavailable')
    )
    vi.mocked(systemManagementApi.tasks)
      .mockRejectedValueOnce(new Error('active unavailable'))
      .mockResolvedValueOnce([{ ...running, status: 'succeeded' }])
    wrapper = mount(Host)
    await flushPromises()
    expect(state.instances.error.value).toBe('nodes unavailable')
    expect(state.active.error.value).toBe('active unavailable')
    expect(state.history.error.value).toBe('')
    expect(state.historyTasks.value).toHaveLength(1)
    await state.instances.refresh()
    expect(state.instances.rows.value).toEqual([node])
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(2)
  })

  it('polls nodes every 30 seconds and recent tasks every 8 seconds only while active', async () => {
    vi.mocked(systemManagementApi.tasks).mockResolvedValue([running])
    wrapper = mount(Host)
    await flushPromises()
    expect(systemManagementApi.tasks).toHaveBeenNthCalledWith(
      1,
      20,
      expect.any(AbortSignal)
    )
    await vi.advanceTimersByTimeAsync(8000)
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(4)
    expect(systemManagementApi.instances).toHaveBeenCalledTimes(1)
    vi.mocked(systemManagementApi.tasks).mockResolvedValue([
      { ...running, status: 'succeeded' },
    ])
    await vi.advanceTimersByTimeAsync(8000)
    expect(state.activeTasks.value).toEqual([])
    expect(state.historyTasks.value).toHaveLength(1)
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(6)
    await vi.advanceTimersByTimeAsync(14000)
    expect(systemManagementApi.instances).toHaveBeenCalledTimes(2)
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(6)
  })

  it('cancels hidden or superseded requests and rejects their late results', async () => {
    let resolveOld!: (value: SystemInstance[]) => void
    vi.mocked(systemManagementApi.instances).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve
        })
    )
    wrapper = mount(Host)
    await flushPromises()
    const oldSignal = vi.mocked(systemManagementApi.instances).mock.calls[0][0]
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(oldSignal?.aborted).toBe(true)
    expect(state.instances.loading.value).toBe(false)
    await vi.advanceTimersByTimeAsync(60000)
    expect(systemManagementApi.instances).toHaveBeenCalledTimes(1)
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(state.instances.rows.value).toEqual([node])
    resolveOld([{ ...node, node_name: 'obsolete' }])
    await flushPromises()
    expect(state.instances.rows.value).toEqual([node])

    vi.mocked(systemManagementApi.instances).mockImplementationOnce(
      () => new Promise(() => {})
    )
    void state.instances.refresh()
    const signal = vi.mocked(systemManagementApi.instances).mock.lastCall?.[0]
    wrapper.unmount()
    wrapper = undefined
    expect(signal?.aborted).toBe(true)
    const count = vi.mocked(systemManagementApi.instances).mock.calls.length
    await vi.advanceTimersByTimeAsync(60000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(systemManagementApi.instances).toHaveBeenCalledTimes(count)
  })

  it('does not start requests when mounted in a hidden document', async () => {
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    wrapper = mount(Host)
    await flushPromises()
    expect(systemManagementApi.instances).not.toHaveBeenCalled()
    expect(systemManagementApi.tasks).not.toHaveBeenCalled()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(systemManagementApi.instances).toHaveBeenCalledTimes(1)
    expect(systemManagementApi.tasks).toHaveBeenCalledTimes(2)
  })
})
