import { flushPromises, mount } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import AutomaticPricingPanel from '../AutomaticPricingPanel.vue'
import {
  automaticPricingApi,
  type AutomaticPricingStatus,
} from '@/api/automaticPricing'
import { parseSystemTask, systemManagementApi } from '@/api/systemManagement'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const status: AutomaticPricingStatus = {
  config: { enabled: false, check_interval_minutes: 30 },
  sources: [
    {
      source: 'sub2api',
      role: 'primary',
      url: 'https://example.com/prices.json',
      hash: 'last-valid-hash',
      checked_at: 1,
      updated_at: 1,
      last_error: 'Source download failed',
      model_count: 2,
    },
  ],
  origins: [],
}
const task = parseSystemTask({
  task_id: 'sync-1',
  type: 'billing_pricing_sync',
  status: 'running',
  created_at: 1,
  updated_at: 1,
})
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.spyOn(automaticPricingApi, 'status').mockImplementation(async () =>
    structuredClone(status)
  )
  vi.spyOn(automaticPricingApi, 'configure').mockImplementation(
    async (value) => value
  )
  vi.spyOn(automaticPricingApi, 'sync').mockResolvedValue(task)
  vi.spyOn(systemManagementApi, 'task').mockResolvedValue(task)
  vi.spyOn(systemManagementApi, 'tasks').mockResolvedValue({
    items: [],
    total: 0,
  })
})
afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})
describe('automatic pricing controls', () => {
  it('reloads a scheduled task on mount, follows it, displays outcomes, and refreshes parent prices once', async () => {
    vi.mocked(systemManagementApi.tasks).mockResolvedValue({
      items: [task],
      total: 1,
    })
    const wrapper = mount(AutomaticPricingPanel, {
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(systemManagementApi.tasks).toHaveBeenCalledWith(
      { type: 'billing_pricing_sync', limit: 1 },
      expect.any(AbortSignal)
    )
    expect(wrapper.text()).toContain('Price check sync-1: Running')
    await wrapper.get('input[type=number]').setValue('75')
    vi.mocked(systemManagementApi.task).mockResolvedValue({
      ...task,
      status: 'failed',
      error: 'One source unavailable',
      pricing_result: {
        checked: 4,
        changed: 1,
        published: 2,
        deactivated: 3,
        unconfigured_count: 101,
        unconfigured: ['unpriced'],
        errors: ['Network unavailable'],
        unconfigured_details: [
          {
            model: 'unpriced',
            reasons: [{ source: 'sub2api', reason: 'Unsupported media cost' }],
          },
        ],
      },
    })
    await vi.advanceTimersByTimeAsync(8000)
    await flushPromises()
    expect(wrapper.get('[data-automatic-result]').text()).toContain(
      'Prices deactivated3'
    )
    expect(wrapper.text()).toContain('Network unavailable')
    expect(wrapper.text()).toContain('Unsupported media cost')
    expect(wrapper.text()).toContain('101')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(
      wrapper.get<HTMLInputElement>('input[type=number]').element.value
    ).toBe('75')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Refresh')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Price check sync-1: Failed')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    wrapper.unmount()
  })

  it('shows task-list failures separately and retries without clearing a schedule draft', async () => {
    vi.mocked(systemManagementApi.tasks).mockRejectedValueOnce(
      new Error('Task list unavailable')
    )
    const wrapper = mount(AutomaticPricingPanel, {
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Task list unavailable')
    expect(wrapper.text()).toContain('last-valid-hash')
    await wrapper.get('input[type=number]').setValue('90')
    vi.mocked(systemManagementApi.tasks).mockResolvedValue({
      items: [{ ...task, status: 'succeeded' }],
      total: 1,
    })
    vi.mocked(systemManagementApi.task).mockResolvedValue({
      ...task,
      status: 'succeeded',
    })
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Refresh task status')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('Task list unavailable')
    expect(wrapper.text()).toContain('Completed')
    expect(
      wrapper.get<HTMLInputElement>('input[type=number]').element.value
    ).toBe('90')
    wrapper.unmount()
  })

  it('does not overwrite a manually started task with a stale initial task list', async () => {
    let resolve!: (value: { items: (typeof task)[]; total: number }) => void
    vi.mocked(systemManagementApi.tasks).mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    const wrapper = mount(AutomaticPricingPanel, {
      global: { plugins: [i18n] },
    })
    await flushPromises()
    await wrapper.get('[data-automatic-sync]').trigger('click')
    await flushPromises()
    resolve({
      items: [{ ...task, task_id: 'older', status: 'succeeded' }],
      total: 1,
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Price check sync-1: Running')
    expect(wrapper.text()).not.toContain('older')
    wrapper.unmount()
  })
  it('shows retained source errors, validates the interval and saves the schedule', async () => {
    const wrapper = mount(AutomaticPricingPanel, {
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Source download failed')
    expect(wrapper.text()).toContain('last-valid-hash')
    await wrapper.get('input[type=number]').setValue('4')
    expect(
      wrapper.get('[data-automatic-save]').attributes('disabled')
    ).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(automaticPricingApi.configure).not.toHaveBeenCalled()
    await wrapper.get('input[type=number]').setValue('1440')
    await wrapper.get('input[type=checkbox]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(automaticPricingApi.configure).toHaveBeenCalledWith({
      enabled: true,
      check_interval_minutes: 1440,
    })
    wrapper.unmount()
  })
  it('checks while disabled, polls the standard task and refreshes sources after completion', async () => {
    const wrapper = mount(AutomaticPricingPanel, {
      global: { plugins: [i18n] },
    })
    await flushPromises()
    await wrapper.get('[data-automatic-sync]').trigger('click')
    await flushPromises()
    expect(automaticPricingApi.sync).toHaveBeenCalledTimes(1)
    expect(systemManagementApi.task).toHaveBeenCalledWith(
      'sync-1',
      expect.any(AbortSignal)
    )
    expect(
      wrapper.get('[data-automatic-sync]').attributes('disabled')
    ).toBeDefined()
    vi.mocked(systemManagementApi.task).mockResolvedValue({
      ...task,
      status: 'failed',
      error: 'No valid source snapshot',
    })
    await vi.advanceTimersByTimeAsync(8000)
    await flushPromises()
    expect(wrapper.text()).toContain('No valid source snapshot')
    expect(automaticPricingApi.status).toHaveBeenCalledTimes(2)
    expect(
      wrapper.get('[data-automatic-sync]').attributes('disabled')
    ).toBeUndefined()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(8000)
    expect(systemManagementApi.task).toHaveBeenCalledTimes(2)
  })
  it('preserves a schedule draft on status refresh and aborts reads when unmounted', async () => {
    const wrapper = mount(AutomaticPricingPanel, {
      global: { plugins: [i18n] },
    })
    await flushPromises()
    await wrapper.get('input[type=number]').setValue('60')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Refresh')!
      .trigger('click')
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('input[type=number]').element.value
    ).toBe('60')
    vi.mocked(automaticPricingApi.status).mockImplementation(
      () => new Promise(() => {})
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Refresh')!
      .trigger('click')
    const signal = vi.mocked(automaticPricingApi.status).mock.calls.at(-1)![0]!
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
})
