import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/console'
import TopupPanel from '@/components/console/wallet/TopupPanel.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

afterEach(() => vi.restoreAllMocks())

async function render(balanceQuota: string) {
  vi.spyOn(api, 'get').mockResolvedValue({
    pay_methods: [{ name: 'Alipay', type: 'alipay' }],
    min_topup: 1,
    amount_options: [10],
  })
  const wrapper = mount(TopupPanel, {
    props: { balanceQuota },
    global: { plugins: [i18n] },
  })
  await flushPromises()
  return wrapper
}

describe('TopupPanel exact balance projection', () => {
  it('adds a purchase to a balance beyond JavaScript safe integer precision', async () => {
    const wrapper = await render('9007199254740993')
    expect(wrapper.text()).toContain('$18,014,398,519.48')
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled')
    ).toBeUndefined()
    wrapper.unmount()
  })

  it('shows an explicit error and blocks purchase when the resulting balance overflows', async () => {
    const post = vi.spyOn(api, 'post')
    const wrapper = await render('9223372036854775807')
    expect(wrapper.get('[role="alert"]').text()).toContain('supported range')
    const submit = wrapper.findAll('button').at(-1)!
    expect(submit.attributes('disabled')).toBeDefined()
    await submit.trigger('click')
    expect(post).not.toHaveBeenCalled()
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
})
