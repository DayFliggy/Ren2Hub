import { createPinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import DrawingLogsView from '@/views/console/DrawingLogsView.vue'
import TaskLogsView from '@/views/console/TaskLogsView.vue'

const { toastError } = vi.hoisted(() => ({ toastError: vi.fn() }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: toastError, success: vi.fn() }),
}))

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
})

beforeEach(async () => {
  await setLocale('zh-CN')
  vi.useFakeTimers()
  toastError.mockClear()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

function deferred<T = unknown>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const views = [
  {
    name: 'drawing',
    view: DrawingLogsView,
    endpoint: '/api/mj/self',
    input: 'drawing-task-search',
    idField: 'mj_id',
    submitTime: 1_790_784_000_000,
  },
  {
    name: 'task',
    view: TaskLogsView,
    endpoint: '/api/task/self',
    input: 'task-log-search',
    idField: 'task_id',
    submitTime: 1_790_784_000,
  },
]

describe.each(views)('$name log query lifecycle', (config) => {
  function page(marker = 'current-result', total = 21) {
    return {
      page: 1,
      page_size: 10,
      total,
      items: [
        {
          id: 1,
          [config.idField]: marker,
          quota: 1000,
          submit_time: config.submitTime,
          status: 'SUCCESS',
          platform: 'test-plugin',
        },
      ],
    }
  }

  async function mountView() {
    wrapper = mount(config.view, {
      global: {
        plugins: [createPinia(), i18n],
        stubs: {
          LogsNavTabs: true,
          AsyncTaskSubTabs: true,
          TaskArtifactsCell: true,
        },
      },
    })
    await flushPromises()
  }

  function assertPending() {
    expect(wrapper!.findComponent({ name: 'DataTable' }).props('rows')).toEqual(
      []
    )
    expect(wrapper!.findComponent({ name: 'DataTable' }).props('loading')).toBe(
      true
    )
    expect(wrapper!.findAll('[role="alert"]')).toHaveLength(0)
    for (const pager of wrapper!.findAllComponents(TablePagination)) {
      expect(pager.props('total')).toBe(0)
    }
    expect(
      wrapper!.find('section[aria-label]').findAll('article')
    ).toHaveLength(0)
  }

  function assertFailure(message: string) {
    const alerts = wrapper!.findAll('[role="alert"]')
    expect(alerts).toHaveLength(2)
    for (const alert of alerts) {
      expect(alert.text()).toContain(i18n.global.t('logs.loadFailed'))
      expect(alert.text()).toContain(message)
      expect(alert.get('button').text()).toBe(i18n.global.t('common.retry'))
    }
    expect(wrapper!.findComponent({ name: 'DataTable' }).exists()).toBe(false)
    expect(wrapper!.findAllComponents(TablePagination)).toHaveLength(0)
    expect(wrapper!.findAll('article')).toHaveLength(0)
  }

  async function changeDate(value = '2026-10-01') {
    wrapper!.findComponent(DateRangePicker).vm.$emit('update:start', value)
    await flushPromises()
  }

  it.each(['zh-CN', 'en'] as const)(
    'shows an initial error and retry for desktop and mobile in %s',
    async (locale) => {
      await setLocale(locale)
      vi.spyOn(api, 'get').mockRejectedValue(new Error('network unavailable'))
      await mountView()

      assertFailure(i18n.global.t('common.failed'))
      expect(toastError).toHaveBeenCalledTimes(1)
    }
  )

  it('clears old results during search debounce, reports failure and retries the same query', async () => {
    const next = deferred()
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce(page('old-result'))
      .mockReturnValueOnce(next.promise)
      .mockResolvedValueOnce(page('retried-result', 1))
    await mountView()
    expect(wrapper!.text()).toContain('old-result')

    await wrapper!.get(`input[name="${config.input}"]`).setValue(' new-query ')
    assertPending()
    expect(wrapper!.text()).not.toContain('old-result')
    expect(get).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(300)
    expect(get.mock.calls[1]![1]).toMatchObject({
      [config.idField]: 'new-query',
      p: 1,
    })
    next.reject(new ApiError('Query unavailable', { status: 503 }))
    await flushPromises()
    assertFailure('Query unavailable')

    await wrapper!.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(get.mock.calls[2]![1]).toEqual(get.mock.calls[1]![1])
    expect(wrapper!.text()).toContain('retried-result')
    expect(wrapper!.findAll('[role="alert"]')).toHaveLength(0)
    expect(wrapper!.findComponent({ name: 'DataTable' }).props('loading')).toBe(
      false
    )
    expect(wrapper!.findComponent(TablePagination).props('total')).toBe(1)
  })

  it('clears old results and reports failure after date filters change', async () => {
    const next = deferred()
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce(page('old-result'))
      .mockReturnValueOnce(next.promise)
    await mountView()
    await changeDate()
    assertPending()
    expect(wrapper!.text()).not.toContain('old-result')

    next.reject(new Error('network unavailable'))
    await flushPromises()
    assertFailure(i18n.global.t('common.failed'))
  })

  it.each(['page', 'pageSize'] as const)(
    'does not retain old results when changing %s fails',
    async (field) => {
      const next = deferred()
      const get = vi
        .spyOn(api, 'get')
        .mockResolvedValueOnce(page('old-result'))
        .mockReturnValueOnce(next.promise)
      await mountView()
      wrapper!
        .findComponent(TablePagination)
        .vm.$emit(`update:${field}`, field === 'page' ? 2 : 20)
      await flushPromises()
      assertPending()
      expect(wrapper!.text()).not.toContain('old-result')
      expect(get.mock.calls[1]![1]).toMatchObject({
        p: field === 'page' ? 2 : 1,
        page_size: field === 'page' ? 10 : 20,
      })

      next.reject(new ApiError('Page unavailable', { status: 503 }))
      await flushPromises()
      assertFailure('Page unavailable')
    }
  )

  it('handles a malformed successful response as a query error', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      page: 1,
      page_size: 10,
      total: 1,
      items: [{ id: 1, quota: 'invalid' }],
    })
    await mountView()

    assertFailure(`Invalid API response: ${config.endpoint}`)
    expect(toastError).toHaveBeenCalledTimes(1)
  })

  it.each(['success', 'failure'] as const)(
    'ignores an old %s after the current query succeeds',
    async (outcome) => {
      const old = deferred()
      const current = deferred()
      const get = vi
        .spyOn(api, 'get')
        .mockReturnValueOnce(old.promise)
        .mockReturnValueOnce(current.promise)
      await mountView()
      const signal = get.mock.calls[0]![2]!.signal
      await changeDate()
      expect(signal?.aborted).toBe(true)
      current.resolve(page('current-result', 1))
      await flushPromises()

      if (outcome === 'success') old.resolve(page('stale-result', 50))
      else old.reject(new ApiError('Stale failure', { status: 503 }))
      await flushPromises()
      expect(wrapper!.text()).toContain('current-result')
      expect(wrapper!.text()).not.toContain('stale-result')
      expect(wrapper!.findComponent(TablePagination).props('total')).toBe(1)
      expect(
        wrapper!.findComponent({ name: 'DataTable' }).props('loading')
      ).toBe(false)
      expect(wrapper!.findAll('[role="alert"]')).toHaveLength(0)
      expect(toastError).not.toHaveBeenCalled()
    }
  )

  it('keeps the current error when an old successful query returns', async () => {
    const old = deferred()
    vi.spyOn(api, 'get')
      .mockReturnValueOnce(old.promise)
      .mockRejectedValueOnce(new ApiError('Current failure', { status: 503 }))
    await mountView()
    await changeDate()
    assertFailure('Current failure')

    old.resolve(page('stale-result'))
    await flushPromises()
    assertFailure('Current failure')
    expect(toastError).toHaveBeenCalledTimes(1)
  })

  it('invalidates old requests immediately while the new search is debounced', async () => {
    const old = deferred()
    const current = deferred()
    const get = vi
      .spyOn(api, 'get')
      .mockReturnValueOnce(old.promise)
      .mockReturnValueOnce(current.promise)
    await mountView()
    await wrapper!.get(`input[name="${config.input}"]`).setValue('new-query')
    expect(get.mock.calls[0]![2]!.signal?.aborted).toBe(true)

    old.resolve(page('stale-result'))
    await flushPromises()
    assertPending()
    expect(get).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(300)
    assertPending()

    current.resolve(page('current-result', 1))
    await flushPromises()
    expect(wrapper!.text()).toContain('current-result')
  })

  it('cancels active requests on unmount without reporting their late failure', async () => {
    const pending = deferred()
    const get = vi.spyOn(api, 'get').mockReturnValue(pending.promise)
    await mountView()
    const signal = get.mock.calls[0]![2]!.signal
    wrapper!.unmount()
    wrapper = null
    expect(signal?.aborted).toBe(true)

    pending.reject(new ApiError('Unmounted failure', { status: 503 }))
    await flushPromises()
    expect(toastError).not.toHaveBeenCalled()
  })

  it('does not start a scheduled search after unmount', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page())
    await mountView()
    await wrapper!.get(`input[name="${config.input}"]`).setValue('new-query')
    wrapper!.unmount()
    wrapper = null
    await vi.advanceTimersByTimeAsync(300)

    expect(get).toHaveBeenCalledTimes(1)
  })
})
