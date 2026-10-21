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
import { nativeTopUp, nativeGmpayEvidence } from '@/api/__tests__/topupFixtures'
import WalletView from '../WalletView.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const state = vi.hoisted(() => ({
  route: { query: {} as Record<string, string> },
  auth: { user: { id: 7, quota: 500000 }, fetchSelf: vi.fn() },
}))
vi.mock('vue-router', () => ({ useRoute: () => state.route }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn() }),
}))
vi.mock('@/api/dashboard', () => ({
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
  state.auth.fetchSelf.mockResolvedValue(undefined)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
})
async function render() {
  const wrapper = mount(WalletView, {
    global: {
      plugins: [i18n],
      stubs: {
        PageHero: true,
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
      ...nativeTopUp,
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
      ...nativeTopUp,
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
      .mockResolvedValue({ ...nativeTopUp, gmpay: nativeGmpayEvidence })
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
