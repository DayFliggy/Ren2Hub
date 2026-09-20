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

afterEach(() => vi.restoreAllMocks())

function quote(amount = 10, expectedQuota = String(amount * 500_000)) {
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

async function render(balanceQuota = '0') {
  vi.spyOn(api, 'get').mockResolvedValue({
    pay_methods: [{ name: 'Alipay', type: 'alipay' }],
    min_topup: 1,
    amount_options: [10, 20],
  })
  if (!vi.isMockFunction(api.post))
    vi.spyOn(api, 'post').mockResolvedValue(quote())
  const wrapper = mount(TopupPanel, {
    props: { balanceQuota, paymentMethod: 'epay:alipay' },
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
