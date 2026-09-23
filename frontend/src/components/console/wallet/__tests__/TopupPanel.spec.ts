import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import TopupPanel from '@/components/console/wallet/TopupPanel.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

const nativeLocation = window.location

afterEach(() => {
  Object.defineProperty(window, 'location', {
    value: nativeLocation,
    configurable: true,
    writable: true,
  })
  vi.restoreAllMocks()
})

interface StubQuote {
  amount: number
  amount_unit: 'USD'
  payment_method: string
  currency: 'CNY' | 'USD'
  amount_minor: string
  pay_amount: string
  expected_quota: string
  min_topup: number
  quote_fingerprint: string
  token?: string
  network?: string
}

function quote(
  amount = 10,
  expectedQuota = String(amount * 500_000)
): StubQuote {
  return {
    amount,
    amount_unit: 'USD',
    payment_method: 'alipay',
    currency: 'CNY',
    amount_minor: String(amount * 730),
    pay_amount: (amount * 7.3).toFixed(2),
    expected_quota: expectedQuota,
    min_topup: 1,
    quote_fingerprint: `quote-${amount}`,
  }
}

/**
 * One USD crypto quote. The panel re-derives gmpay:<token>.<network> from the
 * quote and rejects it when it does not reproduce the selected method.
 */
function cryptoQuote(
  amount = 10,
  overrides: Partial<StubQuote> = {}
): StubQuote {
  return {
    amount,
    amount_unit: 'USD',
    payment_method: 'usdt.tron',
    currency: 'USD',
    amount_minor: String(amount * 100),
    pay_amount: amount.toFixed(2),
    expected_quota: String(amount * 500_000),
    min_topup: 1,
    quote_fingerprint: `crypto-quote-${amount}`,
    token: 'usdt',
    network: 'tron',
    ...overrides,
  }
}

async function render(
  balanceQuota = '0',
  selectedMethod = 'epay:alipay',
  extraMethods: string[] = [],
  epayMinimum = 1
) {
  // Epay methods arrive bare and the panel adds the epay: prefix; crypto assets
  // already carry their gmpay:<token>.<network> identity from the server.
  const methods = [selectedMethod, ...extraMethods]
  vi.spyOn(api, 'get').mockResolvedValue({
    pay_methods: methods.map((value) => {
      const asset = value.replace(/^(?:epay|gmpay):/, '')
      const [token, network] = asset.split('.')
      return value.startsWith('gmpay:')
        ? { name: asset, type: value, token, network, min_topup: 1 }
        : { name: asset, type: asset }
    }),
    min_topup: epayMinimum,
    amount_options: [10, 20],
  })
  if (!vi.isMockFunction(api.post))
    vi.spyOn(api, 'post').mockResolvedValue(quote())
  const wrapper = mount(TopupPanel, {
    props: { balanceQuota, paymentMethod: selectedMethod },
    global: { plugins: [i18n] },
  })
  await flushPromises()
  return wrapper
}

describe('TopupPanel server quotes', () => {
  it('uses the exact server credit for the balance projection and displays CNY separately', async () => {
    vi.spyOn(api, 'post').mockResolvedValue(quote(10, '750000'))
    const wrapper = await render('9007199254740993')
    expect(wrapper.text()).toContain('$18,014,398,510.98')
    expect(wrapper.text()).toContain('¥73.00')
    expect(wrapper.text()).toContain('$1.500')
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeUndefined()
    wrapper.unmount()
  })

  it('shows an explicit error and blocks purchase when the resulting balance overflows', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(quote())
    const wrapper = await render('9223372036854775807')
    expect(wrapper.get('[role="alert"]').text()).toContain('supported range')
    const submit = wrapper.findAll('button').at(-1)!
    expect(submit.attributes('disabled')).toBeDefined()
    await submit.trigger('click')
    expect(post.mock.calls.every(([url]) => url.endsWith('/quote'))).toBe(true)
    wrapper.unmount()
  })

  it('rejects fractional purchase units without throwing during rendering', async () => {
    const wrapper = await render('0')
    await wrapper.get('input').setValue('1.000001')
    expect(wrapper.get('[role="alert"]').text()).toContain('whole USD amount')
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeDefined()
    wrapper.unmount()
  })

  it('ignores older responses even if abort is ignored and clears quotes for invalid input', async () => {
    let completeOld!: (value: ReturnType<typeof quote>) => void
    const post = vi
      .spyOn(api, 'post')
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            completeOld = resolve
          })
      )
      .mockResolvedValueOnce(quote(20))
    const wrapper = await render()
    await wrapper.get('input').setValue('20')
    await flushPromises()
    expect(wrapper.text()).toContain('¥146.00')
    expect(post.mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    completeOld(quote())
    await flushPromises()
    expect(wrapper.text()).toContain('¥146.00')
    expect(wrapper.text()).not.toContain('¥73.00')
    await wrapper.get('input').setValue('')
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeDefined()
    expect(wrapper.text()).not.toContain('¥146.00')
    wrapper.unmount()
  })

  it('shows quote failures and never retains a previously valid quote', async () => {
    vi.spyOn(api, 'post')
      .mockResolvedValueOnce(quote())
      .mockRejectedValueOnce(new Error('offline'))
    const wrapper = await render()
    await wrapper.get('input').setValue('20')
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeDefined()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'could not be loaded'
    )
    expect(wrapper.text()).not.toContain('¥73.00')
    wrapper.unmount()
  })

  it('prices the crypto rail in USD and quotes the bare asset', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(cryptoQuote(10))
    const wrapper = await render('0', 'gmpay:usdt.tron', ['epay:alipay'])
    expect(post).toHaveBeenCalledWith(
      '/api/next/wallet/gmpay/quote',
      { amount: 10, amount_unit: 'USD', payment_method: 'usdt.tron' },
      expect.anything()
    )
    expect(wrapper.text()).toContain('You pay (USD)')
    expect(wrapper.text()).toContain('$10.00')
    expect(wrapper.text()).not.toContain('¥')
    expect(wrapper.text()).toContain('Continue to cashier')
    expect(wrapper.text()).toContain(
      'Pay with the selected token. Before transferring, check the network, address, and amount shown in the cashier.'
    )
    wrapper.unmount()
  })

  it('uses the crypto minimum independently from the Epay minimum', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(cryptoQuote(10))
    const wrapper = await render('0', 'gmpay:usdt.tron', ['epay:alipay'], 50)
    expect(post).toHaveBeenCalledWith(
      '/api/next/wallet/gmpay/quote',
      { amount: 10, amount_unit: 'USD', payment_method: 'usdt.tron' },
      expect.anything()
    )
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeUndefined()
    expect(
      wrapper.findAll('button').some((button) => button.text() === '$10')
    ).toBe(true)
    await wrapper.setProps({ paymentMethod: 'epay:alipay' })
    await flushPromises()
    expect(
      wrapper.findAll('button').some((button) => button.text() === '$10')
    ).toBe(false)
    expect(
      wrapper.findAll('button').some((button) => button.text() === '$50')
    ).toBe(true)
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeDefined()
    wrapper.unmount()
  })

  it.each([
    'javascript:alert(1)',
    'http://gateway.example/pay',
    'https://user:password@gateway.example/pay',
    '/relative-cashier',
  ])('rejects an unsafe crypto cashier address: %s', async (url) => {
    Object.defineProperty(window, 'location', {
      value: { href: 'http://localhost/wallet' },
      configurable: true,
      writable: true,
    })
    vi.spyOn(api, 'post')
      .mockResolvedValueOnce(cryptoQuote())
      .mockResolvedValueOnce({ url })
    const wrapper = await render('0', 'gmpay:usdt.tron')
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()
    expect(window.location.href).toBe('http://localhost/wallet')
    expect(wrapper.emitted('done')).toBeUndefined()
    wrapper.unmount()
  })

  it('leaves the site for the hosted cashier instead of submitting a hidden form', async () => {
    Object.defineProperty(window, 'location', {
      value: { href: 'http://localhost/wallet' },
      configurable: true,
      writable: true,
    })
    const submitForm = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(() => {})
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce(cryptoQuote(10))
      .mockResolvedValueOnce({
        trade_no: 'GP0123456789012345678901234567890',
        status: 'PENDING',
        currency: 'USD',
        pay_amount: '10.00',
        token: 'usdt',
        network: 'tron',
        url: 'https://gateway.example/pay/checkout-counter/cashier-1',
      })
    const wrapper = await render('0', 'gmpay:usdt.tron', ['epay:alipay'])
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith('/api/next/wallet/gmpay/topup', {
      amount: 10,
      amount_unit: 'USD',
      payment_method: 'usdt.tron',
      quote_fingerprint: 'crypto-quote-10',
    })
    expect(window.location.href).toBe(
      'https://gateway.example/pay/checkout-counter/cashier-1'
    )
    expect(submitForm).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('refuses a crypto quote whose frozen asset is not the selected method', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue(cryptoQuote(10, { network: 'ethereum' }))
    const wrapper = await render('0', 'gmpay:usdt.tron', ['epay:alipay'])
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'could not be loaded'
    )
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeDefined()
    expect(post.mock.calls.every(([url]) => url.endsWith('/quote'))).toBe(true)
    wrapper.unmount()
  })

  it('requires another click after QUOTE_CHANGED and submits explicit USD with the new fingerprint', async () => {
    const submitForm = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(() => {})
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce(quote())
      .mockRejectedValueOnce(new ApiError('changed', { code: 'QUOTE_CHANGED' }))
      .mockResolvedValueOnce({
        ...quote(),
        quote_fingerprint: 'new-quote',
        amount_minor: '7400',
        pay_amount: '74.00',
      })
      .mockResolvedValueOnce({
        url: 'https://gateway.example/submit.php',
        data: { test: 'mock' },
      })
    const wrapper = await render()
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('quote changed')
    expect(wrapper.text()).toContain('¥74.00')
    expect(submitForm).not.toHaveBeenCalled()
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/topup'))
    ).toHaveLength(1)
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith('/api/next/wallet/topup', {
      amount: 10,
      amount_unit: 'USD',
      payment_method: 'alipay',
      quote_fingerprint: 'new-quote',
    })
    expect(submitForm).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
})
