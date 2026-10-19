import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
const payment = vi.hoisted(() => ({
  info: vi.fn(),
  quote: vi.fn(),
  pay: vi.fn(),
}))
vi.mock('@/api/topup', async () => ({
  ...(await vi.importActual<typeof import('@/api/topup')>('@/api/topup')),
  topupApi: payment,
}))
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import type { TopupInfo } from '@/api/topup'
import TopupPanel from '../TopupPanel.vue'
const info: TopupInfo = {
  enable_online_topup: true,
  enable_stripe_topup: true,
  enable_creem_topup: false,
  enable_waffo_topup: false,
  enable_waffo_pancake_topup: false,
  enable_gmpay_topup: true,
  enable_redemption: true,
  pay_methods: [
    { type: 'alipay', name: 'Alipay' },
    { type: 'gmpay:usdt.tron', name: 'USDT · Tron' },
    { type: 'stripe', name: 'Stripe' },
  ],
  waffo_pay_methods: null,
  creem_products: '',
  min_topup: 1,
  stripe_min_topup: 1,
  waffo_min_topup: 1,
  waffo_pancake_min_topup: 1,
  amount_options: [10, 25, 50],
  discount: {},
  topup_link: '',
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  useAppStore().status = { quota_display_type: 'USD', quota_per_unit: 500000 }
  payment.info.mockResolvedValue(structuredClone(info))
  payment.quote.mockResolvedValue('69.123400')
  payment.pay.mockResolvedValue({
    url: 'https://pay.example/checkout',
    data: { trade_no: 'order-one' },
  })
})
async function render() {
  const wrapper = mount(TopupPanel, { global: { plugins: [i18n] } })
  await flushPromises()
  return wrapper
}
function button(wrapper: Awaited<ReturnType<typeof render>>, text: string) {
  return wrapper.findAll('button').find((b) => b.text() === text)!
}

describe('historical top-up layout with native payment contracts', () => {
  it.each([
    {
      locale: 'en',
      title: 'Quick top-up',
      placeholder: 'Enter amount',
      balance: 'Balance after',
      callback:
        'Credit arrival is confirmed by the server after verifying payment.',
      submit: 'Top up now',
      loading: 'Updating payment quote…',
      currency: 'Confirm the payment currency at checkout.',
      invalid:
        'Enter a safe integer that meets the minimum quantity and recharge step.',
      retry: 'Retry quote',
      gmpay: 'Continue to cashier',
      network:
        'Pay with the selected token. Before transferring, check the network, address, and amount shown in the cashier.',
    },
    {
      locale: 'zh-CN',
      title: '快捷充值',
      placeholder: '输入金额',
      balance: '到账后余额',
      callback: '服务端核验支付成功后，额度将自动到账。',
      submit: '立即充值',
      loading: '正在更新支付报价…',
      currency: '支付币种以收银台显示为准。',
      invalid: '请输入符合最低数量和充值步长的安全整数。',
      retry: '重新获取报价',
      gmpay: '前往收银台支付',
      network:
        '请使用所选代币付款，并在转账前核对收银台显示的网络、地址和金额。',
    },
  ])(
    'renders restored $locale copy without historical global wallet messages',
    async (copy) => {
      let resolveQuote!: (value: string) => void
      payment.quote.mockImplementationOnce(
        () =>
          new Promise<string>((resolve) => {
            resolveQuote = resolve
          })
      )
      const localI18n = createI18n({
        legacy: false,
        locale: copy.locale,
        fallbackLocale: 'en',
        messages: {
          en: { wallet: { payMethodLabel: 'Payment method' } },
          'zh-CN': { wallet: { payMethodLabel: '支付方式' } },
        },
      })
      const wrapper = mount(TopupPanel, { global: { plugins: [localI18n] } })
      await flushPromises()
      expect(wrapper.text()).toContain(copy.title)
      expect(
        wrapper.get('input[inputmode=numeric]').attributes('placeholder')
      ).toBe(copy.placeholder)
      expect(wrapper.text()).toContain(copy.balance)
      expect(wrapper.text()).toContain(copy.callback)
      expect(wrapper.text()).toContain(copy.loading)
      expect(wrapper.text()).toContain(copy.currency)
      expect(button(wrapper, copy.submit).attributes('disabled')).toBeDefined()
      expect(wrapper.text()).not.toContain('wallet.')
      resolveQuote('69.123400')
      await flushPromises()
      expect(wrapper.text()).not.toContain(copy.loading)
      expect(
        button(wrapper, copy.submit).attributes('disabled')
      ).toBeUndefined()
      await wrapper.get('input[inputmode=numeric]').setValue('10.50')
      await flushPromises()
      expect(wrapper.text()).toContain(copy.invalid)
      payment.quote.mockRejectedValueOnce(new Error('quote unavailable'))
      await wrapper.get('input[inputmode=numeric]').setValue('10')
      await flushPromises()
      expect(button(wrapper, copy.retry).exists()).toBe(true)
      await button(wrapper, 'USDT · Tron').trigger('click')
      await flushPromises()
      expect(button(wrapper, copy.gmpay).exists()).toBe(true)
      expect(wrapper.text()).toContain(copy.network)
      expect(wrapper.text()).not.toContain('wallet.')
      wrapper.unmount()
    }
  )

  it('restores presets, payment buttons and summary rows using the actual server amount', async () => {
    const wrapper = await render()
    expect(wrapper.text()).toContain('Quick top-up')
    expect(button(wrapper, '10').attributes('aria-pressed')).toBe('true')
    expect(button(wrapper, '25').exists()).toBe(true)
    expect(button(wrapper, 'USDT · Tron').exists()).toBe(true)
    expect(wrapper.findAll('select')).toHaveLength(0)
    expect(wrapper.text()).toContain('Credit received')
    expect(wrapper.text()).toContain('Balance after')
    expect(wrapper.text()).toContain('69.123400')
    expect(wrapper.text()).not.toContain('You pay (USD)')
    expect(payment.quote).toHaveBeenCalledWith(
      'alipay',
      10,
      expect.any(AbortSignal)
    )
    expect(wrapper.emitted('capabilities')?.[0]).toEqual([info])
    await button(wrapper, '25').trigger('click')
    await flushPromises()
    expect(payment.quote).toHaveBeenLastCalledWith(
      'alipay',
      25,
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })

  it('keeps token quantities exact and disables unsupported Stripe in token mode', async () => {
    useAppStore().status.quota_display_type = 'TOKENS'
    const wrapper = await render()
    expect(button(wrapper, '5000000').exists()).toBe(true)
    expect(button(wrapper, 'Stripe')).toBeUndefined()
    expect(wrapper.text()).toContain('Stripe top-up is disabled')
    expect(payment.quote).toHaveBeenCalledWith(
      'alipay',
      5000000,
      expect.any(AbortSignal)
    )
    await wrapper.get('input[inputmode=numeric]').setValue('5000001')
    await flushPromises()
    expect(wrapper.text()).toContain('Enter a multiple of 500000 tokens')
    expect(button(wrapper, 'Top up now').attributes('disabled')).toBeDefined()
    expect(payment.quote).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it.each(['10.50', '1e3', '-10', '+10', '10 50'])(
    'keeps invalid quantity %s unchanged without quoting or ordering it',
    async (value) => {
      const wrapper = await render()
      payment.quote.mockClear()
      const input = wrapper.get('input[inputmode=numeric]')
      await input.setValue(value)
      await input.trigger('blur')
      await flushPromises()
      expect(input.element).toHaveProperty('value', value)
      expect(payment.quote).not.toHaveBeenCalled()
      expect(wrapper.text()).not.toContain('69.123400')
      expect(button(wrapper, 'Top up now').attributes('disabled')).toBeDefined()
      await button(wrapper, 'Top up now').trigger('click')
      await flushPromises()
      expect(payment.pay).not.toHaveBeenCalled()

      await input.setValue('42')
      await flushPromises()
      expect(payment.quote).toHaveBeenCalledExactlyOnceWith(
        'alipay',
        42,
        expect.any(AbortSignal)
      )
      expect(
        button(wrapper, 'Top up now').attributes('disabled')
      ).toBeUndefined()
      payment.pay.mockRejectedValueOnce(new Error('mock checkout'))
      await button(wrapper, 'Top up now').trigger('click')
      await flushPromises()
      expect(payment.pay).toHaveBeenCalledExactlyOnceWith(
        'alipay',
        { amount: 42 },
        expect.any(AbortSignal)
      )
      wrapper.unmount()
    }
  )

  it('ignores an outdated quote after changing the quantity', async () => {
    let resolve!: (value: string) => void
    payment.quote.mockImplementationOnce(
      () =>
        new Promise<string>((done) => {
          resolve = done
        })
    )
    const wrapper = await render()
    const signal = payment.quote.mock.calls[0]![2] as AbortSignal
    await button(wrapper, '25').trigger('click')
    await flushPromises()
    expect(signal.aborted).toBe(true)
    resolve('999.00')
    await flushPromises()
    expect(wrapper.text()).toContain('69.123400')
    expect(wrapper.text()).not.toContain('999.00')
    wrapper.unmount()
  })

  it('blocks checkout when the server quote fails or is malformed and supports retry', async () => {
    payment.quote.mockRejectedValueOnce(new Error('quote unavailable'))
    const wrapper = await render()
    expect(wrapper.text()).toContain('quote unavailable')
    expect(button(wrapper, 'Top up now').attributes('disabled')).toBeDefined()
    payment.quote.mockResolvedValueOnce({ amount: '3.00' })
    await button(wrapper, 'Retry quote').trigger('click')
    await flushPromises()
    expect(button(wrapper, 'Top up now').attributes('disabled')).toBeDefined()
    await button(wrapper, 'Retry quote').trigger('click')
    await flushPromises()
    expect(button(wrapper, 'Top up now').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('sends only the native order quantity and submits the validated EPay form', async () => {
    const submit = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(() => {})
    const wrapper = await render()
    await button(wrapper, 'Top up now').trigger('click')
    await flushPromises()
    expect(payment.pay).toHaveBeenCalledExactlyOnceWith(
      'alipay',
      { amount: 10 },
      expect.any(AbortSignal)
    )
    expect(submit).toHaveBeenCalledOnce()
    expect(wrapper.emitted('done')).toHaveLength(1)
    expect(
      document.querySelector('form[action="https://pay.example/checkout"]')
    ).toBeNull()
    wrapper.unmount()
    submit.mockRestore()
  })

  it('preserves provider errors without reporting a completed order or redirecting', async () => {
    payment.pay.mockRejectedValueOnce(new Error('order result unknown'))
    const wrapper = await render()
    await button(wrapper, 'Top up now').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('order result unknown')
    expect(wrapper.emitted('done')).toBeUndefined()
    expect(payment.pay).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('does not treat an invalid checkout URL as a completed order', async () => {
    payment.pay.mockResolvedValueOnce({ url: 'javascript:alert(1)', data: {} })
    const wrapper = await render()
    await button(wrapper, 'Top up now').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Invalid checkout URL')
    expect(wrapper.emitted('done')).toBeUndefined()
    wrapper.unmount()
  })

  it('cancels payment completion after leaving the wallet', async () => {
    let resolve!: (value: unknown) => void
    payment.pay.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    const wrapper = await render()
    await button(wrapper, 'Top up now').trigger('click')
    const signal = payment.pay.mock.calls[0]![2] as AbortSignal
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    resolve({
      url: 'https://pay.example/checkout',
      data: { trade_no: 'late-order' },
    })
    await flushPromises()
    expect(wrapper.emitted('done')).toBeUndefined()
  })

  it('cancels a pending checkout when the site display unit changes even if the quantity stays the same', async () => {
    let resolve!: (value: unknown) => void
    payment.pay.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    const wrapper = await render()
    await button(wrapper, 'Top up now').trigger('click')
    const signal = payment.pay.mock.calls[0]![2] as AbortSignal
    useAppStore().status.quota_display_type = 'CNY'
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.get('input[inputmode=numeric]').element).toHaveProperty(
      'value',
      '10'
    )
    resolve({
      url: 'https://pay.example/checkout',
      data: { trade_no: 'old-unit-order' },
    })
    await flushPromises()
    expect(wrapper.emitted('done')).toBeUndefined()
    wrapper.unmount()
  })

  it('preserves Creem product selection and order payload', async () => {
    const product = {
      productId: 'credits-100',
      name: '100 credits',
      price: 99,
      currency: 'USD',
      quota: 100,
    }
    payment.info.mockResolvedValueOnce({
      ...info,
      enable_creem_topup: true,
      creem_products: JSON.stringify([product]),
    })
    const wrapper = await render()
    await button(wrapper, 'Creem').trigger('click')
    await flushPromises()
    expect(wrapper.get('select').text()).toContain('100 credits · 99 USD')
    expect(wrapper.text()).toContain('99 USD')
    payment.pay.mockRejectedValueOnce(new Error('mock checkout'))
    await button(wrapper, 'Top up now').trigger('click')
    await flushPromises()
    expect(payment.pay).toHaveBeenCalledWith(
      'creem',
      { product_id: 'credits-100' },
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })
})
