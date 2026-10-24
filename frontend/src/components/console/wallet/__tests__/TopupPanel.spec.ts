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
import { httpTransport } from '@/api/httpTransport'
import { nativeTopupInfo } from '@/api/__tests__/topupFixtures'
import TopupPanel from '../TopupPanel.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { configureQuotaDisplay, formatQuota } from '@/utils/format'

const app = vi.hoisted(() => ({
  status: { quota_display_type: 'USD', quota_per_unit: 500000 },
}))
const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
const nativeLocation = window.location
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  app.status.quota_display_type = 'USD'
  sessionStorage.clear()
})
afterEach(() => {
  Object.defineProperty(window, 'location', {
    value: nativeLocation,
    configurable: true,
    writable: true,
  })
  vi.restoreAllMocks()
  configureQuotaDisplay({})
})
async function render(method = 'gmpay:usdt.tron') {
  vi.spyOn(api, 'get').mockResolvedValue(nativeTopupInfo)
  const wrapper = mount(TopupPanel, {
    props: { paymentMethod: method, balanceQuota: '500000' },
    global: { plugins: [i18n] },
  })
  await flushPromises()
  return wrapper
}
function submit(wrapper: Awaited<ReturnType<typeof render>>) {
  return wrapper
    .findAll('button')
    .find((button) => button.text() === i18n.global.t('wallet.gmpaySubmit'))!
}
describe('native wallet checkout', () => {
  it('shows Creem native quota once and adds it exactly to the account balance', async () => {
    configureQuotaDisplay({ quota_per_unit: 500000, quota_display_type: 'USD' })
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopupInfo,
      enable_creem_topup: true,
      creem_products: JSON.stringify([
        {
          productId: 'prod_test',
          name: 'Credits',
          price: 9,
          currency: 'USD',
          quota: 1500000,
        },
      ]),
    })
    const quote = vi.spyOn(httpTransport, 'request')
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'creem', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain(formatQuota(1500000, 3))
    expect(wrapper.text()).toContain(formatQuota(2000000))
    expect(quote).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('quotes CNY and submits the complete crypto method exactly once', async () => {
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://console.example', assign },
      configurable: true,
      writable: true,
    })
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockImplementation(async (_method, path) =>
        path === '/api/user/amount'
          ? ({ message: 'success', data: '73.00' } as never)
          : ({
              message: 'success',
              url: 'https://cashier.example/pay',
              data: {
                trade_no: 'GPtest',
                currency: 'CNY',
                pay_amount: '73.00',
                expected_quota: 5000000,
                token: 'usdt',
                network: 'tron',
              },
            } as never)
      )
    const wrapper = await render()
    expect(wrapper.text()).toContain('¥73.00')
    await submit(wrapper).trigger('click')
    await flushPromises()
    expect(
      request.mock.calls.filter(([, path]) => path === '/api/user/gmpay/pay')
    ).toHaveLength(1)
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/gmpay/pay', {
      data: { amount: 10, payment_method: 'gmpay:usdt.tron' },
      signal: undefined,
    })
    expect(sessionStorage.getItem('gmpay-order:7')).toBe('GPtest')
    expect(wrapper.emitted('track-order')).toEqual([['GPtest']])
    expect(assign).toHaveBeenCalledWith('https://cashier.example/pay')
    wrapper.unmount()
  })
  it('retains an uncertain durable order and blocks another create, including after input changes', async () => {
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockImplementation(async (_method, path) =>
        path === '/api/user/amount'
          ? ({ message: 'success', data: '73.00' } as never)
          : ({
              message: 'error',
              data: 'Creation failed',
              trade_no: 'GPtest',
              creation_status: 'unknown',
            } as never)
      )
    const wrapper = await render()
    await submit(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.emitted('track-order')).toEqual([['GPtest']])
    expect(submit(wrapper).attributes('disabled')).toBeDefined()
    await wrapper.get('input').setValue('20')
    await flushPromises()
    await submit(wrapper).trigger('click')
    await flushPromises()
    expect(
      request.mock.calls.filter(([, path]) => path === '/api/user/gmpay/pay')
    ).toHaveLength(1)
    wrapper.unmount()
  })
  it('never retries a transport failure and does not invent an order number', async () => {
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockImplementation(async (_method, path) => {
        if (path === '/api/user/amount')
          return { message: 'success', data: '73.00' } as never
        throw new Error('Connection interrupted')
      })
    const wrapper = await render()
    await submit(wrapper).trigger('click')
    await flushPromises()
    expect(submit(wrapper).attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('track-order')).toBeUndefined()
    expect(
      request.mock.calls.filter(([, path]) => path === '/api/user/gmpay/pay')
    ).toHaveLength(1)
    wrapper.unmount()
  })
  it('uses scaled token quantities and rejects partial native units', async () => {
    app.status.quota_display_type = 'TOKENS'
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValue({ message: 'success', data: '73.00' } as never)
    const wrapper = await render()
    expect(request).toHaveBeenLastCalledWith(
      'POST',
      '/api/user/amount',
      expect.objectContaining({
        data: { amount: 5000000, payment_method: 'gmpay:usdt.tron' },
      })
    )
    await wrapper.get('input').setValue('750001')
    await flushPromises()
    expect(submit(wrapper).attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
