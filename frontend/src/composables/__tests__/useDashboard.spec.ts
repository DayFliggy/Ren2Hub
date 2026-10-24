import { defineComponent } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { api } from '@/api/client'
import { useDashboard } from '@/composables/useDashboard'
import { dashboardFixture } from '@/api/__tests__/dashboardFixture'
import i18n, { loadMessageDomain } from '@/i18n'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
const app = vi.hoisted(() => ({ status: { log_max_range_days: 365 } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { quota: 500, used_quota: 250, request_count: 9 },
  }),
}))
let wrapper: VueWrapper | undefined
function setupDashboard() {
  let state!: ReturnType<typeof useDashboard>
  wrapper = mount(
    defineComponent({
      setup() {
        state = useDashboard()
        return () => null
      },
    }),
    { global: { plugins: [i18n] } }
  )
  return state
}
// A block body: a returned mock would run as teardown and record a stray call.
beforeAll(async () => {
  await loadMessageDomain('console')
})
beforeEach(() => {
  vi.mocked(api.get).mockResolvedValue(dashboardFixture())
})
afterEach(() => {
  wrapper?.unmount()
  vi.resetAllMocks()
  vi.useRealTimers()
  app.status.log_max_range_days = 365
})
describe('native overview summary', () => {
  it('uses real account quota and three native summary periods without supplemental resource reads', async () => {
    const state = setupDashboard()
    await state.load()
    expect(state.stats.value).toMatchObject({
      quota: '500',
      used_quota: '250',
      today_quota: '25',
      total_requests: 9,
      month_quota: '25',
    })
    expect(api.get).toHaveBeenCalledTimes(3)
    // No overview card shows a delta, so none of the windows compares.
    expect(
      vi
        .mocked(api.get)
        .mock.calls.every(
          ([url, params]) =>
            url === '/api/data/self/summary' &&
            params?.include_comparison === 'false'
        )
    ).toBe(true)
    expect(state.tokenTrend.value[0]!.cache_read).toBe(4)
    expect(state.flushIntervalMinutes.value).toBe(5)
  })
  it('keeps account KPIs and marks only usage-backed values unavailable', async () => {
    const state = setupDashboard()
    await state.load()
    const unavailable = dashboardFixture()
    unavailable.meta.available = false
    vi.mocked(api.get).mockResolvedValue(unavailable)
    await state.load()
    expect(state.stats.value).toMatchObject({
      quota: '500',
      used_quota: '250',
      total_requests: 9,
    })
    expect(state.stats.value?.today_quota).toBeUndefined()
    expect(state.stats.value?.today_requests).toBeUndefined()
    expect(state.stats.value?.today_tokens).toBeUndefined()
    expect(state.flow.value).toEqual([])
    expect(state.tokenTrend.value).toEqual([])
    expect(state.error.value).toBeNull()
    expect(state.usageUnavailable.value).toBe(true)
  })
  it('skips a month past the log limit and keeps the other periods', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 31, 12))
    app.status.log_max_range_days = 30
    const state = setupDashboard()
    await state.load()
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(
      vi.mocked(api.get).mock.calls.map(([, params]) => params?.range)
    ).toEqual(['30d', 'today'])
    expect(state.error.value).toBeNull()
    expect(state.stats.value?.today_quota).toBe('25')
    expect(state.stats.value?.month_quota).toBeUndefined()
  })
  it('skips a 30-day window past the log limit and keeps today and the balance', async () => {
    // Day 31 also puts the month past a 20-day limit.
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 31, 12))
    app.status.log_max_range_days = 20
    const state = setupDashboard()
    await state.load()
    expect(
      vi.mocked(api.get).mock.calls.map(([, params]) => params?.range)
    ).toEqual(['today'])
    expect(state.error.value).toBeNull()
    expect(state.stats.value).toMatchObject({
      quota: '500',
      used_quota: '250',
      today_quota: '25',
      today_requests: 2,
      today_tokens: 10,
    })
    expect(state.stats.value?.month_quota).toBeUndefined()
    expect(state.recentOutOfRange.value).toBe(
      i18n.global.t('dashboard.stats.recentOutOfRange', { days: 20 })
    )
    expect(state.share.value).toEqual([])
    expect(state.flow.value).toEqual([])
    expect(state.tokenTrend.value).toEqual([])
  })
  it('keeps a legal month window when only the 30-day trend exceeds the limit', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 5, 12))
    app.status.log_max_range_days = 20
    const state = setupDashboard()
    await state.load()

    expect(
      vi.mocked(api.get).mock.calls.map(([, params]) => params?.range)
    ).toEqual(['custom', 'today'])
    expect(state.stats.value?.month_quota).toBe('25')
    expect(state.stats.value?.today_quota).toBe('25')
    expect(state.recentOutOfRange.value).toBeTruthy()
    expect(state.error.value).toBeNull()
  })
  it('reads today tokens independently of the latest trend point', async () => {
    vi.mocked(api.get).mockImplementation(async (_url, params) =>
      dashboardFixture(params?.range === 'today' ? 123 : 900)
    )
    const state = setupDashboard()
    await state.load()

    expect(state.stats.value?.today_tokens).toBe(123)
    expect(state.tokenTrend.value.at(-1)?.total).toBe(900)
  })
  it('clears the out-of-range notice once the 30-day window fits again', async () => {
    app.status.log_max_range_days = 20
    const state = setupDashboard()
    await state.load()
    app.status.log_max_range_days = 365
    await state.load()
    expect(state.recentOutOfRange.value).toBeNull()
    expect(state.tokenTrend.value).not.toEqual([])
  })
})
