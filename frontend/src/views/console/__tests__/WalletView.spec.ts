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
import { api } from '@/api/console'
import { getUsageSummary } from '@/api/dashboard'
import {
  nativeTopUpDetail,
  nativeGmpayEvidence,
  nativeTopupInfo,
} from '@/api/__tests__/topupFixtures'
import WalletView from '../WalletView.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { configureQuotaDisplay, formatQuota } from '@/utils/format'

const state = vi.hoisted(() => ({
  route: { query: {} as Record<string, string> },
  auth: { user: { id: 7, quota: 500000 as number | null }, fetchSelf: vi.fn() },
  app: { status: { log_max_range_days: 365 } },
}))
vi.mock('vue-router', () => ({ useRoute: () => state.route }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn() }),
}))
vi.mock('@/api/dashboard', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/dashboard')>()),
  getUsageSummary: vi.fn(async () => ({
    kpi: { totalQuota: '0', totalRequests: 0 },
    flow: [],
    comparison: { quotaDelta: null, requestsDelta: null },
    meta: { available: true },
  })),
}))
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  sessionStorage.clear()
  state.route.query = {}
  state.auth.user.id = 7
  state.auth.user.quota = 500000
  state.auth.fetchSelf.mockResolvedValue(undefined)
  state.app.status.log_max_range_days = 365
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  configureQuotaDisplay({})
})
async function render() {
  const wrapper = mount(WalletView, {
    global: {
      plugins: [i18n],
      stubs: {
        PageHero: { template: '<div><slot name="actions" /></div>' },
        StatTileGrid: true,
        TopupPanel: true,
        RedeemPanel: true,
        TopupRecords: true,
        FlowChart: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}
describe('wallet payment return', () => {
  it('queries the authorized native order and refreshes the balance only after server-confirmed completion', async () => {
    state.route.query = { trade_no: 'GPtest' }
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopUpDetail,
      status: 'success',
      credited_quota: 5000000,
      complete_time: 1792368100,
      gmpay: { ...nativeGmpayEvidence, status: 'settled' },
    })
    const post = vi.spyOn(api, 'post')
    const wrapper = await render()
    expect(get).toHaveBeenCalledWith(
      '/api/user/topup/detail/GPtest',
      undefined,
      { signal: expect.any(AbortSignal) }
    )
    expect(state.auth.fetchSelf).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('Completed')
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('ignores an untrusted success query and preserves the server unknown state without recreating payment', async () => {
    state.route.query = { trade_no: 'GPtest', status: 'success' }
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopUpDetail,
      gmpay: nativeGmpayEvidence,
    })
    const post = vi.spyOn(api, 'post')
    const wrapper = await render()
    expect(wrapper.text()).toContain('Pending')
    expect(wrapper.text()).not.toContain('Completed')
    expect(state.auth.fetchSelf).not.toHaveBeenCalled()
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('recovers the same user checkout after a gateway return without a trade number', async () => {
    sessionStorage.setItem('gmpay-order:7', 'GPtest')
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ ...nativeTopUpDetail, gmpay: nativeGmpayEvidence })
    const wrapper = await render()
    expect(get).toHaveBeenCalledWith(
      '/api/user/topup/detail/GPtest',
      undefined,
      { signal: expect.any(AbortSignal) }
    )
    wrapper.unmount()
    get.mockClear()
    state.auth.user.id = 8
    const other = await render()
    expect(get).not.toHaveBeenCalled()
    other.unmount()
  })
})
describe('wallet balance header', () => {
  const headerText = (wrapper: Awaited<ReturnType<typeof render>>) =>
    wrapper.get('p.font-mono').text().replace(/\s+/g, '')
  it.each(['disabled', 'failed'])(
    'keeps the account balance when statistics are %s',
    async (state) => {
      const get = vi.mocked(getUsageSummary)
      if (state === 'failed')
        get.mockRejectedValueOnce(new Error('Unavailable'))
      else get.mockResolvedValueOnce({ meta: { available: false } } as never)
      const wrapper = await render()
      expect(headerText(wrapper)).toBe(formatQuota(500000))
      expect(
        wrapper.findComponent({ name: 'TopupPanel' }).props('balanceQuota')
      ).toBe('500000')
      expect(wrapper.findComponent({ name: 'FlowChart' }).exists()).toBe(false)
      expect(
        wrapper.findComponent({ name: 'StatTileGrid' }).props('tiles')
      ).toEqual([])
      wrapper.unmount()
    }
  )
  it('distinguishes unknown balance from a true zero', async () => {
    state.auth.user.quota = null
    const unknown = await render()
    expect(headerText(unknown)).toBe('—')
    unknown.unmount()
    state.auth.user.quota = 0
    const zero = await render()
    expect(headerText(zero)).toBe(formatQuota(0))
    zero.unmount()
  })
  it('uses the configured currency and the same text as formatQuota', async () => {
    configureQuotaDisplay({ quota_display_type: 'CNY', usd_exchange_rate: 7.3 })
    state.auth.user.quota = 617_283_950
    const wrapper = await render()
    expect(headerText(wrapper)).toBe('¥9,012.35')
    expect(headerText(wrapper)).toBe(formatQuota(617_283_950))
    expect(wrapper.text()).not.toContain('$')
    wrapper.unmount()
  })
  it('shows grouped raw tokens without a currency symbol or decimals', async () => {
    configureQuotaDisplay({ quota_display_type: 'TOKENS' })
    state.auth.user.quota = 617_283_950
    const wrapper = await render()
    expect(headerText(wrapper)).toBe('617,283,950')
    expect(headerText(wrapper)).toBe(formatQuota(617_283_950))
    wrapper.unmount()
  })
})
describe('wallet month statistics', () => {
  afterEach(() => vi.useRealTimers())
  const tiles = (wrapper: Awaited<ReturnType<typeof render>>) =>
    wrapper.findComponent({ name: 'StatTileGrid' }).props('tiles') as {
      value: string
      sub: string
    }[]
  it('asks for the month to date without a comparison', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 31, 12))
    const wrapper = await render()
    expect(getUsageSummary).toHaveBeenCalledWith(
      { range: 'custom', start: '2026-10-01', end: '2026-10-31' },
      false,
      expect.any(AbortSignal)
    )
    expect(getUsageSummary).toHaveBeenCalledWith(
      { range: 'today' },
      false,
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })
  it('marks month figures out of range when the month exceeds the log limit', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 31, 12))
    state.app.status.log_max_range_days = 30
    const wrapper = await render()
    expect(getUsageSummary).toHaveBeenCalledOnce()
    expect(getUsageSummary).toHaveBeenCalledWith(
      { range: 'today' },
      false,
      expect.any(AbortSignal)
    )
    const [month, average, today, requests] = tiles(wrapper)
    for (const tile of [month!, average!, requests!]) {
      expect(tile).toMatchObject({
        value: '—',
        sub: 'This month exceeds the log query limit',
      })
    }
    expect(today!.value).not.toBe('—')
    wrapper.unmount()
  })
})

describe('wallet payment capabilities layout', () => {
  it('keeps one column while capabilities load or fail, then reveals only enabled redemption', async () => {
    const wrapper = await render()
    const panel = wrapper.findComponent({ name: 'TopupPanel' })
    const region = () => panel.element.parentElement!
    expect(region().getAttribute('aria-busy')).toBe('true')
    expect(region().classList.contains('xl:grid-cols-[1fr_23.75rem]')).toBe(
      false
    )
    expect(wrapper.findComponent({ name: 'RedeemPanel' }).exists()).toBe(false)

    panel.vm.$emit('capabilities-state', 'failed')
    await flushPromises()
    expect(region().getAttribute('aria-busy')).toBe('false')
    expect(region().classList.contains('xl:grid-cols-[1fr_23.75rem]')).toBe(
      false
    )

    panel.vm.$emit('capabilities', {
      ...nativeTopupInfo,
      enable_redemption: false,
    })
    panel.vm.$emit('capabilities-state', 'ready')
    await flushPromises()
    expect(region().classList.contains('xl:grid-cols-[1fr_23.75rem]')).toBe(
      false
    )
    expect(wrapper.findComponent({ name: 'RedeemPanel' }).exists()).toBe(false)

    panel.vm.$emit('capabilities', {
      ...nativeTopupInfo,
      enable_redemption: true,
    })
    await flushPromises()
    expect(region().classList.contains('xl:grid-cols-[1fr_23.75rem]')).toBe(
      true
    )
    expect(wrapper.findComponent({ name: 'RedeemPanel' }).exists()).toBe(true)
    wrapper.unmount()
  })
})
