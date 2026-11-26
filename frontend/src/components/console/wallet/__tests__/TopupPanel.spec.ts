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
/** A server quote for $10 without bonus, unless patched. */
function quoted(payAmount: string, patch: Record<string, unknown> = {}) {
  const quote = {
    ...quoteResponse.quote,
    principal_quota: 5000000,
    bonus_quota: 0,
    expected_quota: 5000000,
    pay_amount: payAmount,
    applied_rate: '0',
    amount_rate: '0',
    fixed_rate: '0',
    ...patch,
  }
  return { message: 'success', data: quote.pay_amount, quote }
}
/** The preview of a provider-priced credit of principal with bonus. */
function preview(principal: number, bonus: number) {
  return {
    ...quoteResponse.quote,
    principal_quota: principal,
    bonus_quota: bonus,
    expected_quota: principal + bonus,
  }
}
/** Answers topup/info with info and the bonus preview with previews. */
function routeGet(
  info: Record<string, unknown>,
  previews: (principal: string) => unknown
) {
  return vi
    .spyOn(api, 'get')
    .mockImplementation(async (path, params) =>
      path === '/api/user/topup-bonus/preview'
        ? (previews(
            String((params as Record<string, unknown>).principal_quota)
          ) as never)
        : (info as never)
    )
}
function payCalls(request: { mock: { calls: unknown[][] } }, path: string) {
  return request.mock.calls.filter(([, called]) => called === path)
}
describe('native wallet checkout', () => {
  it('lists every backup payment as a new-tab web link and drops other URLs', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopupInfo,
      backup_payment_links: [
        { name: '', url: 'https://codes.example.com', icon: '' },
        {
          name: 'Card shop',
          url: 'https://shop.example.com/buy',
          icon: 'taobao',
        },
        { name: 'Script', url: 'javascript:alert(1)', icon: '' },
      ],
    })
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'alipay', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    const links = wrapper.findAll('a')
    expect(links.map((link) => link.attributes('href'))).toEqual([
      'https://codes.example.com/',
      'https://shop.example.com/buy',
    ])
    expect(links[0]!.text()).toContain(i18n.global.t('wallet.externalTitle'))
    expect(links[1]!.text()).toContain('Card shop')
    for (const link of links) {
      expect(link.attributes('target')).toBe('_blank')
      expect(link.attributes('rel')).toContain('noopener')
    }
    wrapper.unmount()
  })

  it('asks for the payment method before the amount and announces the choice', async () => {
    const wrapper = await render('alipay')
    const group = wrapper.get('[role="group"]')
    const methods = group.findAll('button')
    const firstPreset = wrapper
      .findAll('button[aria-pressed]')
      .find((button) => !group.element.contains(button.element))!
    expect(
      group.element.compareDocumentPosition(firstPreset.element) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(methods.map((button) => button.attributes('aria-pressed'))).toEqual([
      'true',
      'false',
    ])
    await methods[1]!.trigger('click')
    expect(wrapper.emitted('update:paymentMethod')).toEqual([
      ['gmpay:usdt.tron'],
    ])
    wrapper.unmount()
  })

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

  it('shows the Creem product quota with its previewed bonus and credits both exactly', async () => {
    configureQuotaDisplay({ quota_per_unit: 500000, quota_display_type: 'USD' })
    const get = routeGet(
      {
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
      },
      (principal) => preview(Number(principal), 75000)
    )
    const quote = vi.spyOn(httpTransport, 'request')
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'creem', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(get).toHaveBeenCalledWith(
      '/api/user/topup-bonus/preview',
      { principal_quota: '1500000' },
      expect.anything()
    )
    expect(wrapper.text()).toContain(
      i18n.global.t('wallet.creditBonus', { amount: formatQuota(75000, 3) })
    )
    expect(wrapper.text()).toContain(formatQuota(1575000, 3))
    // The balance after paying is no longer shown.
    expect(wrapper.text()).not.toContain(formatQuota(2075000))
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
    // No bonus, so no bonus line under the credit.
    expect(wrapper.text()).not.toContain(
      i18n.global.t('wallet.creditBonus', { amount: formatQuota(0, 3) })
    )
    await submit(wrapper).trigger('click')
    await flushPromises()
    expect(payCalls(request, '/api/user/gmpay/pay')).toHaveLength(1)
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/gmpay/pay', {
      data: {
        amount: 10,
        expected_bonus_quota: 0,
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
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    const submethod = wrapper.get('[data-testid="topup-submethod"]')
    await submethod.get('button').trigger('click')
    await flushPromises()
    const options = wrapper
      .findAll('[role="option"]')
      .map((option) => option.text().trim())
    expect(options.slice(1)).toEqual(['Card', 'Apple Pay'])
    expect(wrapper.text()).toContain('EUR 10.00')
    // The payable label names no currency; the amount carries the quote's.
    expect(wrapper.text()).not.toContain('CNY')
    await wrapper
      .findAll('[role="option"]')
      .find((option) => option.text().includes('Apple Pay'))!
      .trigger('click')
    await flushPromises()
    await submit(wrapper, 'wallet.topupNow').trigger('click')
    await flushPromises()
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/waffo/pay', {
      data: {
        amount: 10,
        expected_bonus_quota: 0,
        expected_pay_amount: '10.00',
        expected_currency: 'EUR',
        pay_method_index: 1,
        payment_method: 'waffo',
      },
      signal: undefined,
    })
    wrapper.unmount()
  })

  it('keeps the price and shows the credit with its bonus from the server quote', async () => {
    configureQuotaDisplay({ quota_per_unit: 500000, quota_display_type: 'USD' })
    vi.spyOn(httpTransport, 'request').mockResolvedValue(quoteResponse as never)
    const wrapper = await render('alipay')
    const summary = wrapper.text()
    expect(summary).toContain('¥730.00')
    expect(wrapper.find('.line-through').exists()).toBe(false)
    expect(summary).toContain(
      i18n.global.t('wallet.creditBonus', { amount: formatQuota(2500000, 3) })
    )
    // The rate stays on the preset card instead of repeating in the receipt.
    expect(wrapper.get('dl').text()).not.toContain('5%')
    // Credit is the server's principal plus bonus, not the requested amount.
    expect(summary).toContain(formatQuota(52500000, 3))
    wrapper.unmount()
  })

  it('marks presets with the server bonus rate, the selected one from its quote', async () => {
    configureQuotaDisplay({ quota_per_unit: 500000, quota_display_type: 'USD' })
    routeGet(
      { ...nativeTopupInfo, amount_options: [10, 20, 50, 100] },
      (principal) => {
        if (principal === '10000000')
          return { ...preview(10000000, 0), applied_rate: '0' }
        if (principal === '25000000') return Promise.reject(new Error('down'))
        return {
          ...preview(Number(principal), Number(principal) / 50),
          applied_rate: '0.02',
        }
      }
    )
    // The $10 quote carries a 5% bonus, so its card shows 5% rather than the preview.
    vi.spyOn(httpTransport, 'request').mockResolvedValue(quoteResponse as never)
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'alipay', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    const rates = wrapper
      .get(
        `[role="group"][aria-label="${i18n.global.t('wallet.presetAmounts')}"]`
      )
      .findAll('button')
      .map((button) => button.find('span[aria-hidden="true"]'))
      .map((rate) => (rate.exists() ? rate.text() : null))
    expect(rates).toEqual(['+5%', null, null, '+2%'])
    // The balance after paying is no longer shown.
    expect(wrapper.text()).not.toContain(formatQuota(53000000))
    wrapper.unmount()
  })

  it('previews token presets at their own quota and never blocks paying when previews fail', async () => {
    app.status.quota_display_type = 'TOKENS'
    const get = routeGet({ ...nativeTopupInfo, amount_options: [10] }, () =>
      Promise.reject(new Error('down'))
    )
    vi.spyOn(httpTransport, 'request').mockResolvedValue(
      quoted('73.00') as never
    )
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'alipay', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    // Ten native units are already 5,000,000 tokens; they are not scaled again.
    expect(get).toHaveBeenCalledWith(
      '/api/user/topup-bonus/preview',
      { principal_quota: '5000000' },
      expect.anything()
    )
    expect(
      submit(wrapper, 'wallet.topupNow').attributes('disabled')
    ).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps the newest preset rates when an older preview round answers late', async () => {
    configureQuotaDisplay({ quota_per_unit: 500000, quota_display_type: 'USD' })
    const pending: Array<{
      principal: string
      resolve: (rate: string) => void
    }> = []
    routeGet(
      {
        ...nativeTopupInfo,
        amount_options: [10, 20],
        pay_methods: [
          { type: 'alipay', name: 'Alipay' },
          { type: 'gmpay:usdt.tron', name: 'USDT Tron', min_topup: '20' },
        ],
      },
      (principal) =>
        new Promise((resolve) =>
          pending.push({
            principal,
            resolve: (rate) =>
              resolve({
                ...preview(Number(principal), Number(principal) / 50),
                applied_rate: rate,
              }),
          })
        )
    )
    vi.spyOn(httpTransport, 'request').mockReturnValue(new Promise(() => {}))
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'alipay', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    const firstRound = pending.splice(0)
    await wrapper.setProps({ paymentMethod: 'gmpay:usdt.tron' })
    await flushPromises()
    for (const call of pending) call.resolve('0.02')
    for (const call of firstRound) call.resolve('0.05')
    await flushPromises()
    const rates = wrapper
      .get(
        `[role="group"][aria-label="${i18n.global.t('wallet.presetAmounts')}"]`
      )
      .findAll('span[aria-hidden="true"]')
      .map((rate) => rate.text())
    expect(rates).toEqual(['+2%'])
    wrapper.unmount()
  })

  it('confirms the previewed Stripe bonus without a price confirmation', async () => {
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://console.example', assign },
      configurable: true,
      writable: true,
    })
    routeGet(
      {
        ...nativeTopupInfo,
        enable_stripe_topup: true,
        pay_methods: [{ type: 'stripe', name: 'Stripe' }],
      },
      (principal) => preview(Number(principal), 100000)
    )
    let bonusChanged = true
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockImplementation(async (_method, path) => {
        if (path === '/api/user/stripe/amount')
          return { message: 'success', data: '10.00' } as never
        if (bonusChanged) {
          bonusChanged = false
          return {
            message: 'error',
            code: 'topup_bonus_changed',
            data: 'The top-up bonus changed, please confirm the new bonus',
            bonus: preview(5000000, 150000),
          } as never
        }
        return {
          message: 'success',
          data: { pay_link: 'https://checkout.stripe.example/s' },
        } as never
      })
    const wrapper = mount(TopupPanel, {
      props: { paymentMethod: 'stripe', balanceQuota: '500000' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('10.00')
    expect(wrapper.text()).toContain(formatQuota(5100000, 3))
    await submit(wrapper, 'wallet.topupNow').trigger('click')
    await flushPromises()
    expect(assign).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(`+${formatQuota(150000, 3)}`)
    expect(wrapper.text()).toContain(i18n.global.t('wallet.quoteChanged'))
    await submit(wrapper, 'wallet.topupNow').trigger('click')
    await flushPromises()
    const pays = payCalls(request, '/api/user/stripe/pay')
    expect(pays).toHaveLength(2)
    expect(pays[0]![2]).toMatchObject({
      data: { expected_bonus_quota: 100000 },
    })
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/stripe/pay', {
      data: {
        amount: 10,
        expected_bonus_quota: 150000,
        success_url: 'https://console.example/wallet',
        cancel_url: 'https://console.example/wallet',
        payment_method: 'stripe',
      },
      signal: undefined,
    })
    expect(assign).toHaveBeenCalledWith('https://checkout.stripe.example/s')
    wrapper.unmount()
  })

  it('shows a changed quote without paying or blocking, then confirms it on the next click', async () => {
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
    expect(wrapper.text()).toContain('¥730.00')
    expect(wrapper.text()).toContain(`+${formatQuota(2500000, 3)}`)
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
      data: {
        expected_pay_amount: '73.00',
        expected_currency: 'CNY',
        expected_bonus_quota: 0,
      },
    })
    expect(pays[1]![2]).toMatchObject({
      data: {
        expected_pay_amount: '730.00',
        expected_currency: 'CNY',
        expected_bonus_quota: 2500000,
      },
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

  it.each([
    ['with other methods', [{ type: 'alipay', name: 'Alipay' }]],
    ['as the only method', []],
  ])(
    'explains why Stripe is hidden in TOKENS display %s',
    async (_case, others) => {
      app.status.quota_display_type = 'TOKENS'
      vi.spyOn(api, 'get').mockResolvedValue({
        ...nativeTopupInfo,
        enable_stripe_topup: true,
        pay_methods: [{ type: 'stripe', name: 'Stripe' }, ...others],
      })
      const wrapper = mount(TopupPanel, {
        props: { paymentMethod: '', balanceQuota: '500000' },
        global: { plugins: [i18n] },
      })
      await flushPromises()
      const note = wrapper.get('[data-stripe-tokens-unavailable]').text()
      expect(note).toContain('Stripe top-up is disabled in this mode')
      expect(wrapper.text().replace(note, '')).not.toContain('Stripe')
      expect(wrapper.text()).not.toContain('Online payments are not enabled')
      wrapper.unmount()
    }
  )

  it('does not mention Stripe when it is not enabled in TOKENS display', async () => {
    app.status.quota_display_type = 'TOKENS'
    const wrapper = await render()
    expect(wrapper.text()).not.toContain('Stripe')
    wrapper.unmount()
  })
})
