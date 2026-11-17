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
}))
vi.mock('vue-router', () => ({ useRoute: () => state.route }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn() }),
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
        TopupPanel: true,
        RedeemPanel: true,
        TopupRecords: true,
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
describe('wallet payment capabilities layout', () => {
  it('places redemption between top-up and records only once the server enables it', async () => {
    const wrapper = await render()
    const panel = wrapper.findComponent({ name: 'TopupPanel' })
    const order = () =>
      Array.from((panel.element as Element).parentElement!.children).map(
        (element) => element.tagName.toLowerCase()
      )
    expect(wrapper.findComponent({ name: 'RedeemPanel' }).exists()).toBe(false)

    panel.vm.$emit('capabilities', {
      ...nativeTopupInfo,
      enable_redemption: true,
    })
    panel.vm.$emit('capabilities-state', 'failed')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'RedeemPanel' }).exists()).toBe(false)

    panel.vm.$emit('capabilities', {
      ...nativeTopupInfo,
      enable_redemption: false,
    })
    panel.vm.$emit('capabilities-state', 'ready')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'RedeemPanel' }).exists()).toBe(false)

    panel.vm.$emit('capabilities', {
      ...nativeTopupInfo,
      enable_redemption: true,
    })
    await flushPromises()
    expect(order()).toEqual([
      'topup-panel-stub',
      'redeem-panel-stub',
      'topup-records-stub',
    ])
    wrapper.unmount()
  })
})
