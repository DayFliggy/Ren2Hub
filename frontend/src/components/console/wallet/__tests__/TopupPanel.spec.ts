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
import quoteResponse from '../../../../../tests/fixtures/native/topup-quote.post.json'
import quoteChangedResponse from '../../../../../tests/fixtures/native/topup-quote-changed.post.json'
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
function submit(
  wrapper: Awaited<ReturnType<typeof render>>,
  label = 'wallet.gmpaySubmit'
) {
  return wrapper
    .findAll('button')
    .find((button) => button.text() === i18n.global.t(label))!
}
/** An undiscounted server quote for $10, unless patched. */
function quoted(payAmount: string, patch: Record<string, unknown> = {}) {
  const quote = {
    ...quoteResponse.quote,
    expected_quota: 5000000,
    list_pay_amount: payAmount,
    pay_amount: payAmount,
    factor: '1',
    applied_rate: '0',
    amount_rate: '0',
    fixed_rate: '0',
    ...patch,
  }
  return { message: 'success', data: quote.pay_amount, quote }
}
function payCalls(request: { mock: { calls: unknown[][] } }, path: string) {
  return request.mock.calls.filter(([, called]) => called === path)
}
describe('native wallet checkout', () => {
  it('cancels capability loading on unmount without publishing a late ready state', async () => {
    const onCapabilitiesState = vi.fn()
    const onCapabilities = vi.fn()
    let finish!: (value: typeof nativeTopupInfo) => void
    const get = vi.spyOn(api, 'get').mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const wrapper = mount(TopupPanel, {
      props: {
        paymentMethod: '',
        balanceQuota: '500000',
        onCapabilitiesState,
        onCapabilities,
      },
      global: { plugins: [i18n] },
    })
    const signal = get.mock.calls[0]![2]?.signal as AbortSignal
    expect(signal.aborted).toBe(false)
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    finish(nativeTopupInfo)
    await flushPromises()
    expect(onCapabilitiesState.mock.calls).toEqual([['idle']])
    expect(onCapabilities).not.toHaveBeenCalled()
  })

  it('reports configuration loading, failure and retry without leaving a false loading placeholder', async () => {
    let reject!: (error: Error) => void
    const get = vi.spyOn(api, 'get').mockImplementationOnce(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail
        })
    )
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: '', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[role="status"]').text()).toBe(
      i18n.global.t('common.loading')
    )
    expect(wrapper.emitted('capabilities-state')).toEqual([['idle']])
    expect(wrapper.emitted('capabilities')).toBeUndefined()
    reject(new Error('Unavailable'))
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(1)
    expect(wrapper.text()).not.toContain(
      i18n.global.t('wallet.invalidTopupAmount')
    )
    expect(wrapper.text()).toContain(
      i18n.global.t('wallet.paymentMethodsUnavailable')
    )
    expect(wrapper.emitted('capabilities-state')).toEqual([
      ['idle'],
      ['failed'],
    ])
    get.mockResolvedValue({
      ...nativeTopupInfo,
      pay_methods: [],
      enable_online_topup: false,
    })
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('capabilities-state')).toEqual([
      ['idle'],
      ['failed'],
      ['idle'],
      ['ready'],
    ])
    expect(wrapper.emitted('capabilities')).toHaveLength(1)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    wrapper.unmount()
  })

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
          ? (quoted('73.00') as never)
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
    expect(wrapper.text()).toContain(formatQuota(5000000, 3))
    expect(wrapper.text()).not.toContain(
      i18n.global.t('wallet.summaryListPrice')
    )
    await submit(wrapper).trigger('click')
    await flushPromises()
    expect(payCalls(request, '/api/user/gmpay/pay')).toHaveLength(1)
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/gmpay/pay', {
      data: {
        amount: 10,
        expected_pay_amount: '73.00',
        expected_currency: 'CNY',
        payment_method: 'gmpay:usdt.tron',
      },
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
          ? (quoted('73.00') as never)
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
        if (path === '/api/user/amount') return quoted('73.00') as never
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
  it('labels Waffo sub-methods by name and confirms the quoted Waffo currency', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopupInfo,
      enable_waffo_topup: true,
      pay_methods: [{ type: 'waffo', name: 'Waffo' }],
      // Native defaults: Card has an empty payMethodName and Apple Pay shares no key.
      waffo_pay_methods: [
        {
          name: 'Card',
          icon: '/pay-card.png',
          payMethodType: 'CREDITCARD,DEBITCARD',
          payMethodName: '',
        },
        {
          name: 'Apple Pay',
          icon: '/pay-apple.png',
          payMethodType: 'APPLEPAY',
          payMethodName: 'APPLEPAY',
        },
      ],
    })
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://console.example', assign: vi.fn() },
      configurable: true,
      writable: true,
    })
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockImplementation(async (_method, path) =>
        path === '/api/user/waffo/amount'
          ? (quoted('10.00', { currency: 'EUR' }) as never)
          : ({
              message: 'success',
              data: { payment_url: 'https://cashier.example/waffo' },
            } as never)
      )
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'waffo', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    const options = wrapper
      .get('#topup-submethod')
      .findAll('option')
      .map((option) => option.text())
    expect(options.slice(1)).toEqual(['Card', 'Apple Pay'])
    expect(wrapper.text()).toContain(i18n.global.t('wallet.nativeCurrencyNote'))
    expect(wrapper.text()).toContain('EUR 10.00')
    expect(wrapper.text()).not.toContain(i18n.global.t('wallet.summaryPayNow'))
    await wrapper.get('#topup-submethod').setValue('1')
    await submit(wrapper, 'wallet.topupNow').trigger('click')
    await flushPromises()
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/waffo/pay', {
      data: {
        amount: 10,
        expected_pay_amount: '10.00',
        expected_currency: 'EUR',
        pay_method_index: 1,
        payment_method: 'waffo',
      },
      signal: undefined,
    })
    wrapper.unmount()
  })

  it('shows the list price and exact discount percent from the server quote', async () => {
    configureQuotaDisplay({ quota_per_unit: 500000, quota_display_type: 'USD' })
    vi.spyOn(httpTransport, 'request').mockResolvedValue({
      ...quoteResponse,
      data: '720.87',
      quote: {
        ...quoteResponse.quote,
        applied_rate: '0.0125',
        pay_amount: '720.87',
      },
    } as never)
    const wrapper = await render('alipay')
    const summary = wrapper.text()
    expect(summary).toContain(i18n.global.t('wallet.summaryListPrice'))
    expect(summary).toContain('¥730.00')
    expect(summary).toContain(
      i18n.global.t('wallet.summaryDiscountRate', { rate: '1.25' })
    )
    expect(summary).toContain('¥720.87')
    // Credit is the server's expected quota, not the requested amount.
    expect(summary).toContain(formatQuota(50000000, 3))
    wrapper.unmount()
  })

  it('sends no price confirmation for the undiscounted Stripe checkout', async () => {
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://console.example', assign },
      configurable: true,
      writable: true,
    })
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopupInfo,
      enable_stripe_topup: true,
      pay_methods: [{ type: 'stripe', name: 'Stripe' }],
    })
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockImplementation(async (_method, path) =>
        path === '/api/user/stripe/amount'
          ? ({ message: 'success', data: '10.00' } as never)
          : ({
              message: 'success',
              data: { pay_link: 'https://checkout.stripe.example/s' },
            } as never)
      )
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'stripe', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('10.00')
    expect(wrapper.text()).toContain(formatQuota(5000000, 3))
    await submit(wrapper, 'wallet.topupNow').trigger('click')
    await flushPromises()
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/stripe/pay', {
      data: {
        amount: 10,
        success_url: 'https://console.example/wallet',
        cancel_url: 'https://console.example/wallet',
        payment_method: 'stripe',
      },
      signal: undefined,
    })
    expect(assign).toHaveBeenCalledWith('https://checkout.stripe.example/s')
    wrapper.unmount()
  })

  it('shows a changed price without paying or blocking, then confirms it on the next click', async () => {
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://console.example', assign },
      configurable: true,
      writable: true,
    })
    let changed = true
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockImplementation(async (_method, path) => {
        if (path === '/api/user/amount') return quoted('73.00') as never
        if (changed) {
          changed = false
          return quoteChangedResponse as never
        }
        return {
          message: 'success',
          url: 'https://cashier.example/pay',
          data: { trade_no: 'GPtest', creation_status: 'ready' },
        } as never
      })
    const wrapper = await render()
    await submit(wrapper).trigger('click')
    await flushPromises()
    expect(assign).not.toHaveBeenCalled()
    expect(wrapper.emitted('track-order')).toBeUndefined()
    expect(wrapper.text()).toContain('¥693.50')
    expect(wrapper.text()).toContain('¥730.00')
    expect(wrapper.text()).toContain(i18n.global.t('wallet.quoteChanged'))
    expect(wrapper.text()).not.toContain(
      i18n.global.t('wallet.creationUncertain')
    )
    expect(submit(wrapper).attributes('disabled')).toBeUndefined()
    await submit(wrapper).trigger('click')
    await flushPromises()
    const pays = payCalls(request, '/api/user/gmpay/pay')
    expect(pays).toHaveLength(2)
    expect(pays[0]![2]).toMatchObject({
      data: { expected_pay_amount: '73.00', expected_currency: 'CNY' },
    })
    expect(pays[1]![2]).toMatchObject({
      data: { expected_pay_amount: '693.50', expected_currency: 'CNY' },
    })
    expect(wrapper.emitted('track-order')).toEqual([['GPtest']])
    expect(assign).toHaveBeenCalledWith('https://cashier.example/pay')
    wrapper.unmount()
  })

  it('uses scaled token quantities and rejects partial native units', async () => {
    app.status.quota_display_type = 'TOKENS'
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValue(quoted('73.00') as never)
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
