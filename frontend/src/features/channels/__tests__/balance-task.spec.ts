import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { api } from '@/api/client'
import BalanceUpdateTask from '../components/BalanceUpdateTask.vue'
const global = {
  plugins: [
    createI18n({
      legacy: false,
      locale: 'en',
      missingWarn: false,
      fallbackWarn: false,
    }),
  ],
}
afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})
describe('background balance update', () => {
  it('starts once, follows progress and stops on completion', async () => {
    vi.useFakeTimers()
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce({
        started: true,
        task_id: 'task',
        status: 'pending',
      })
      .mockResolvedValueOnce({
        task_id: 'task',
        status: 'running',
        state: { total: 3, processed: 1 },
      })
      .mockResolvedValueOnce({
        task_id: 'task',
        status: 'succeeded',
        state: { total: 3, processed: 3 },
      })
    const wrapper = mount(BalanceUpdateTask, { global })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(4000)
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(3)
    expect(wrapper.emitted('complete')).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(8000)
    expect(get).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })
  it('shows failures without restarting a write and aborts on leave', async () => {
    vi.useFakeTimers()
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce({ task_id: 'task', status: 'pending' })
      .mockRejectedValueOnce(new Error('connection lost'))
    const wrapper = mount(BalanceUpdateTask, { global })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.get('[role="alert"]').text()).toContain('connection lost')
    expect(
      get.mock.calls.filter((call) => call[0] === '/api/channel/update_balance')
    ).toHaveLength(1)
    const signal = (get.mock.calls[0]![2] as { signal: AbortSignal }).signal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(5000)
    expect(get).toHaveBeenCalledTimes(2)
  })
})
