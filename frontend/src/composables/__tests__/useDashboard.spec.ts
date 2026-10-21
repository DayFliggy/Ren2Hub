import { defineComponent } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { useDashboard } from '@/composables/useDashboard'
import { dashboardFixture } from '@/api/__tests__/dashboardFixture'
import i18n from '@/i18n'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
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
beforeEach(() => vi.mocked(api.get).mockResolvedValue(dashboardFixture()))
afterEach(() => {
  wrapper?.unmount()
  vi.resetAllMocks()
})
describe('native overview summary', () => {
  it('uses real account quota and three native summary periods with unknown system data', async () => {
    const state = setupDashboard()
    await state.load()
    expect(state.stats.value).toMatchObject({
      quota: '500',
      used_quota: '250',
      today_quota: '25',
      total_requests: 9,
      month_quota_delta: null,
    })
    expect(api.get).toHaveBeenCalledTimes(3)
    expect(
      vi
        .mocked(api.get)
        .mock.calls.every(([url]) => url === '/api/data/self/summary')
    ).toBe(true)
    expect(state.tokenTrend.value[0]!.input).toBeNull()
    expect(state.system.value).toBeNull()
    expect(state.discounts.value).toBeNull()
    expect(state.limits.value).toBeNull()
  })
  it('clears old data when consumption logging becomes unavailable', async () => {
    const state = setupDashboard()
    await state.load()
    const unavailable = dashboardFixture()
    unavailable.meta.available = false
    vi.mocked(api.get).mockResolvedValue(unavailable)
    await state.load()
    expect(state.stats.value).toBeNull()
    expect(state.flow.value).toEqual([])
    expect(state.tokenTrend.value).toEqual([])
    expect(state.error.value).toBeTruthy()
  })
})
