import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
const mocks = vi.hoisted(() => ({
  info: vi.fn(),
  quote: vi.fn(),
  pay: vi.fn(),
  checkout: vi.fn(),
  status: { quota_display_type: 'USD', quota_per_unit: 500000 },
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ status: mocks.status }),
}))
vi.mock('@/api/topup', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/api/topup')>()
  return { ...original, topupApi: mocks, checkoutTarget: mocks.checkout }
})
import TopupPanel from './TopupPanel.vue'
const info = {
  enable_online_topup: true,
  enable_gmpay_topup: true,
  enable_creem_topup: false,
  enable_stripe_topup: true,
  enable_waffo_topup: true,
  enable_waffo_pancake_topup: true,
  stripe_min_topup: 1,
  waffo_min_topup: 1,
  waffo_pancake_min_topup: 1,
  pay_methods: [
    { type: 'alipay', name: 'Alipay' },
    { type: 'stripe', name: 'Stripe' },
    { type: 'waffo', name: 'Waffo' },
    { type: 'waffo_pancake', name: 'Pancake' },
    { type: 'gmpay:usdt.tron', name: 'USDT' },
  ],
  min_topup: 1,
  amount_options: [10],
  creem_products: '',
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.status.quota_display_type = 'USD'
  mocks.status.quota_per_unit = 500000
  mocks.info.mockResolvedValue(info)
  mocks.quote.mockResolvedValue('73.00')
})
async function render() {
  const wrapper = mount(TopupPanel, {
    global: { plugins: [createI18n({ legacy: false, locale: 'en' })] },
  })
  await flushPromises()
  return wrapper
}
it('ignores a superseded quote after the payment method changes', async () => {
  const pending = deferred<string>()
  mocks.quote.mockReturnValue(pending.promise)
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Calculate amount due')!
    .trigger('click')
  await wrapper.get('select').setValue('gmpay:usdt.tron')
  pending.resolve('999.00')
  await flushPromises()
  expect(wrapper.text()).not.toContain('999.00')
  expect(mocks.quote.mock.calls[0]![2].aborted).toBe(true)
  wrapper.unmount()
})
it('locks inputs during checkout and aborts late navigation when the page unmounts', async () => {
  const pending = deferred<{ url: string; data: object }>()
  mocks.pay.mockReturnValue(pending.promise)
  const wrapper = await render()
  await wrapper.get('select').setValue('gmpay:usdt.tron')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Continue to checkout')!
    .trigger('click')
  expect(wrapper.get('select').attributes('disabled')).toBeDefined()
  expect(wrapper.get('input').attributes('disabled')).toBeDefined()
  expect(mocks.pay).toHaveBeenCalledWith(
    'gmpay:usdt.tron',
    { amount: 10 },
    expect.any(AbortSignal)
  )
  const signal = mocks.pay.mock.calls[0]![2] as AbortSignal
  wrapper.unmount()
  pending.resolve({ url: 'https://pay.example', data: {} })
  await flushPromises()
  expect(signal.aborted).toBe(true)
  expect(mocks.checkout).not.toHaveBeenCalled()
})
it('shows a server rejection and permits retry without reporting payment success', async () => {
  mocks.pay.mockRejectedValue(new Error('Payment disabled'))
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Continue to checkout')!
    .trigger('click')
  await flushPromises()
  expect(wrapper.get('[role=alert]').text()).toContain('Payment disabled')
  expect(wrapper.emitted('done')).toBeUndefined()
  expect(wrapper.get('select').attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
it('does not offer malformed Creem products as a successful empty catalog', async () => {
  mocks.info.mockResolvedValue({
    ...info,
    enable_creem_topup: true,
    creem_products: 'invalid',
  })
  const wrapper = await render()
  expect(wrapper.find('[role=alert]').exists()).toBe(true)
  expect(wrapper.emitted('capabilities')).toBeUndefined()
  wrapper.unmount()
})

for (const display of ['USD', 'TOKENS'])
  for (const method of [
    'alipay',
    'gmpay:usdt.tron',
    'waffo',
    'waffo_pancake',
  ]) {
    it(`uses one ${display} quantity projection for ${method} quote and payment`, async () => {
      mocks.status.quota_display_type = display
      mocks.pay.mockRejectedValue(new Error('test checkout rejection'))
      const wrapper = await render(),
        expected = display === 'TOKENS' ? 5000000 : 10
      await wrapper.get('select').setValue(method)
      expect(wrapper.get('input').element.value).toBe(String(expected))
      expect(wrapper.get('input').attributes('step')).toBe(
        display === 'TOKENS' ? '500000' : '1'
      )
      await wrapper
        .findAll('button')
        .find((b) => b.text() === 'Calculate amount due')!
        .trigger('click')
      await flushPromises()
      expect(mocks.quote).toHaveBeenCalledWith(
        method,
        expected,
        expect.any(AbortSignal)
      )
      await wrapper
        .findAll('button')
        .find((b) => b.text() === 'Continue to checkout')!
        .trigger('click')
      await flushPromises()
      expect(mocks.pay).toHaveBeenCalledWith(
        method,
        { amount: expected },
        expect.any(AbortSignal)
      )
      wrapper.unmount()
    })
  }
it('keeps Stripe visible but rejects every token-mode quote and checkout attempt', async () => {
  mocks.status.quota_display_type = 'TOKENS'
  const wrapper = await render()
  expect(
    wrapper.get('option[value="stripe"]').attributes('disabled')
  ).toBeDefined()
  await wrapper.get('select').setValue('stripe')
  expect(wrapper.text()).toContain('Stripe top-up is disabled')
  for (const label of ['Calculate amount due', 'Continue to checkout']) {
    const button = wrapper.findAll('button').find((b) => b.text() === label)!
    expect(button.attributes('disabled')).toBeDefined()
    await button.trigger('click')
  }
  expect(mocks.quote).not.toHaveBeenCalled()
  expect(mocks.pay).not.toHaveBeenCalled()
  wrapper.unmount()
})
it.each(['alipay', 'gmpay:usdt.tron', 'waffo', 'waffo_pancake'])(
  'rejects token quantities that would be truncated by %s',
  async (method) => {
    mocks.status.quota_display_type = 'TOKENS'
    const wrapper = await render()
    await wrapper.get('select').setValue(method)
    await wrapper.get('input').setValue('750000')
    expect(wrapper.get('[role=alert]').text()).toContain('multiple of 500000')
    for (const label of ['Calculate amount due', 'Continue to checkout']) {
      const button = wrapper.findAll('button').find((b) => b.text() === label)!
      expect(button.attributes('disabled')).toBeDefined()
      await button.trigger('click')
    }
    expect(mocks.quote).not.toHaveBeenCalled()
    expect(mocks.pay).not.toHaveBeenCalled()
    wrapper.unmount()
  }
)
