import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { dashboardFixture } from '@/api/__tests__/dashboardFixture'
import { parseTopupBonusSelf, topupBonusApi } from '@/api/topupBonus'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import DashboardView from '@/views/console/DashboardView.vue'
import selfResponse from '../../../../tests/fixtures/native/topup-bonus-self.get.json'

vi.mock('@/api/client', async (original) => ({
  ...(await original<typeof import('@/api/client')>()),
  api: { get: vi.fn() },
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    status: { log_max_range_days: 365 },
    isFeatureEnabled: (feature: string) =>
      feature === 'admin' && adminFeature.enabled,
  }),
}))
const adminFeature = vi.hoisted(() => ({ enabled: true }))
vi.mock('@/composables/useDashboardResources', () => ({
  useDashboardResources: () => ({
    system: ref(null),
    systemLoading: ref(false),
    systemError: ref(null),
  }),
}))

let wrapper: VueWrapper
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
beforeEach(() => {
  adminFeature.enabled = true
  setActivePinia(createPinia())
  vi.mocked(api.get).mockImplementation(async (path) =>
    dashboardFixture(path === '/api/data/summary' ? 999 : 10)
  )
  vi.spyOn(topupBonusApi, 'self').mockResolvedValue(
    parseTopupBonusSelf(selfResponse.data)
  )
})
afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
  vi.resetAllMocks()
})

function login(role: number, id = 1) {
  useAuthStore().persist({
    id,
    role,
    username: 'viewer',
    display_name: 'Viewer',
    email: '',
    status: 1,
    quota: 500,
    used_quota: 250,
    request_count: 9,
    permissions: { admin_permissions: {} },
  })
}
async function render(role: number) {
  login(role)
  wrapper = mount(DashboardView, {
    global: {
      plugins: [i18n],
      stubs: Object.fromEntries(
        [
          'Breadcrumb',
          'OverviewKpiStrip',
          'BalanceCard',
          'BonusCard',
          'UsageDistributionCard',
          'TokenTrendCard',
          'ModelDistributionCard',
          'SystemStatusCard',
          'StatsKpiRow',
          'StatsModelTable',
          'StatsHourlyChart',
          'StatsDualTrend',
          'AutoRoutePanel',
          'DateRangePicker',
        ].map((name) => [name, true])
      ),
    },
  })
  await flushPromises()
}
function allScope() {
  return wrapper.get('[aria-label="统计范围"] [role="radio"]:last-child')
}
function statsTab() {
  return wrapper.get('#dashboard-tab-panel-tab-stats')
}

it.each([1, 10, 100])(
  'only offers bonus settings to Root role %i',
  async (role) => {
    await render(role)
    expect(
      wrapper.getComponent({ name: 'BonusCard' }).props('canConfigure')
    ).toBe(role === 100)
  }
)

it('hides the bonus settings shortcut when the admin feature is disabled', async () => {
  adminFeature.enabled = false
  await render(100)
  expect(
    wrapper.getComponent({ name: 'BonusCard' }).props('canConfigure')
  ).toBe(false)
})

it.each([1, 10, 100])('exposes automatic routing to role %i', async (role) => {
  await render(role)
  expect(wrapper.find('[aria-label="统计范围"]').exists()).toBe(role >= 10)
  await wrapper.get('#dashboard-tab-panel-tab-autoroute').trigger('click')
  expect(wrapper.find('auto-route-panel-stub').exists()).toBe(true)
  expect(wrapper.find('[aria-label="统计范围"]').exists()).toBe(false)
})

it.each([10, 100])(
  'switches every usage panel for role %i without presenting personal assets as global',
  async (role) => {
    await render(role)
    expect(wrapper.find('balance-card-stub').exists()).toBe(true)
    expect(wrapper.find('token-trend-card-stub').classes()).toContain(
      'xl:col-span-2'
    )
    expect(wrapper.find('model-distribution-card-stub').classes()).toContain(
      'xl:col-span-2'
    )
    vi.mocked(api.get).mockClear()
    vi.mocked(topupBonusApi.self).mockClear()
    await allScope().trigger('click')
    await flushPromises()
    expect(wrapper.attributes('data-dashboard-scope')).toBe('all')
    expect(wrapper.find('balance-card-stub').exists()).toBe(false)
    expect(wrapper.find('bonus-card-stub').exists()).toBe(false)
    expect(wrapper.find('token-trend-card-stub').classes()).toContain(
      'xl:col-span-3'
    )
    expect(wrapper.find('model-distribution-card-stub').classes()).toContain(
      'xl:col-span-3'
    )
    expect(topupBonusApi.self).not.toHaveBeenCalled()
    expect(api.get).toHaveBeenCalledTimes(4)
    expect(
      vi
        .mocked(api.get)
        .mock.calls.every(([path]) => path === '/api/data/summary')
    ).toBe(true)
    expect(
      wrapper.getComponent({ name: 'OverviewKpiStrip' }).props('stats')
    ).toMatchObject({ today_tokens: 999 })
    expect(
      wrapper.getComponent({ name: 'OverviewKpiStrip' }).props('stats')
    ).not.toHaveProperty('quota')
    await statsTab().trigger('click')
    await flushPromises()
    expect(
      wrapper.getComponent({ name: 'StatsKpiRow' }).props('kpi').totalTokens
    ).toBe(999)
    await wrapper
      .get('[aria-label="统计范围"] [role="radio"]:first-child')
      .trigger('click')
    await flushPromises()
    expect(
      wrapper.getComponent({ name: 'StatsKpiRow' }).props('kpi').totalTokens
    ).toBe(10)
    await wrapper.get('#dashboard-tab-panel-tab-overview').trigger('click')
    expect(wrapper.find('balance-card-stub').exists()).toBe(true)
  }
)

it('clears hidden statistics and heatmap immediately and rejects late global responses after demotion', async () => {
  await render(10)
  await statsTab().trigger('click')
  await flushPromises()
  const pending: ((value: ReturnType<typeof dashboardFixture>) => void)[] = []
  vi.mocked(api.get).mockImplementation((path) =>
    path === '/api/data/summary'
      ? new Promise((resolve) => pending.push(resolve))
      : Promise.resolve(dashboardFixture(10))
  )
  await allScope().trigger('click')
  expect(wrapper.getComponent({ name: 'StatsKpiRow' }).props('kpi')).toBeNull()
  const signals = vi
    .mocked(api.get)
    .mock.calls.filter(([path]) => path === '/api/data/summary')
    .map(([, , options]) => options?.signal)
  login(1)
  await flushPromises()
  expect(wrapper.find('[aria-label="统计范围"]').exists()).toBe(false)
  expect(signals.every((signal) => signal?.aborted)).toBe(true)
  pending.forEach((resolve) => resolve(dashboardFixture(999)))
  await flushPromises()
  expect(wrapper.attributes('data-dashboard-scope')).toBe('self')
  expect(
    wrapper.getComponent({ name: 'StatsKpiRow' }).props('kpi').totalTokens
  ).toBe(10)
  await wrapper.get('#dashboard-tab-panel-tab-overview').trigger('click')
  expect(
    wrapper.getComponent({ name: 'UsageDistributionCard' }).props('points')[0]
      .tokens
  ).toBe(10)
})

it('resets global scope when the account changes and on remount', async () => {
  await render(100)
  await allScope().trigger('click')
  await flushPromises()
  login(100, 2)
  await flushPromises()
  expect(wrapper.attributes('data-dashboard-scope')).toBe('self')
  await allScope().trigger('click')
  await flushPromises()
  wrapper.unmount()
  await render(100)
  expect(wrapper.attributes('data-dashboard-scope')).toBe('self')
})
