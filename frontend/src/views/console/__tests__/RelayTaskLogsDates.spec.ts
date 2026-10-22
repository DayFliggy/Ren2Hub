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
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import DrawingLogsView from '@/views/console/DrawingLogsView.vue'
import TaskLogsView from '@/views/console/TaskLogsView.vue'

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

// Fixed zone so the expected bounds below are independent constants:
// 2026-10-01 00:00 and 2026-10-02 23:59:59.999 in Asia/Shanghai.
const OCT_1_START_MS = 1_790_784_000_000
const OCT_2_END_MS = 1_790_956_799_999

beforeEach(() => {
  vi.stubEnv('TZ', 'Asia/Shanghai')
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
  vi.unstubAllEnvs()
})

// Native list pages answer {page, page_size, total, items} (common.PageInfo).
function emptyPage() {
  return Promise.resolve({ page: 1, page_size: 10, total: 0, items: [] })
}

async function mountWithDates(
  view: typeof DrawingLogsView,
  endpoint: string
): Promise<ReturnType<typeof vi.fn>> {
  const get = vi.spyOn(api, 'get').mockImplementation((path: string) => {
    if (path === endpoint) return emptyPage()
    return Promise.reject(new Error(`Unexpected API request: ${path}`))
  })
  wrapper = mount(view, {
    global: {
      plugins: [createPinia(), i18n],
      stubs: { LogsNavTabs: true },
    },
  })
  await flushPromises()
  return get as unknown as ReturnType<typeof vi.fn>
}

async function pickDates(start: string, end: string) {
  const picker = wrapper!.findComponent(DateRangePicker)
  picker.vm.$emit('update:start', start)
  picker.vm.$emit('update:end', end)
  await flushPromises()
}

function lastParams(get: ReturnType<typeof vi.fn>): Record<string, unknown> {
  return get.mock.calls.at(-1)![1] as Record<string, unknown>
}

describe('relay task log date filters', () => {
  it('omits drawing timestamps until dates are picked, then sends local-day milliseconds', async () => {
    const get = await mountWithDates(DrawingLogsView, '/api/mj/self')
    // Native /api/mj treats any sent value, even 0, as a submit_time bound.
    expect(lastParams(get)).not.toHaveProperty('start_timestamp', 0)
    expect(lastParams(get).start_timestamp).toBeUndefined()
    expect(lastParams(get).end_timestamp).toBeUndefined()

    await pickDates('2026-10-01', '2026-10-02')
    expect(lastParams(get).start_timestamp).toBe(OCT_1_START_MS)
    expect(lastParams(get).end_timestamp).toBe(OCT_2_END_MS)

    await pickDates('2026-10-01', '')
    expect(lastParams(get).start_timestamp).toBe(OCT_1_START_MS)
    expect(lastParams(get).end_timestamp).toBeUndefined()

    await pickDates('', '2026-10-02')
    expect(lastParams(get).start_timestamp).toBeUndefined()
    expect(lastParams(get).end_timestamp).toBe(OCT_2_END_MS)
  })

  it('sends task timestamps in seconds only for picked dates', async () => {
    const get = await mountWithDates(TaskLogsView, '/api/task/self')
    expect(lastParams(get).start_timestamp).toBeUndefined()
    expect(lastParams(get).end_timestamp).toBeUndefined()

    await pickDates('2026-10-01', '2026-10-02')
    expect(lastParams(get).start_timestamp).toBe(OCT_1_START_MS / 1000)
    expect(lastParams(get).end_timestamp).toBe(Math.floor(OCT_2_END_MS / 1000))
  })
})
