import { mount } from '@vue/test-utils'
import { beforeAll, expect, it } from 'vitest'

import PaymentMethodShare from '@/components/console/orders/PaymentMethodShare.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

it('labels providers and legacy payment methods without merging them', () => {
  const wrapper = mount(PaymentMethodShare, {
    props: {
      currency: 'CNY',
      items: [
        { method: 'epay', amount: 60, count: 3 },
        { method: 'alipay', amount: 30, count: 2 },
        { method: 'custom_rail', amount: 10, count: 1 },
      ],
    },
    global: { plugins: [i18n] },
  })
  const rows = wrapper.findAll('li').map((row) => row.text())
  expect(rows[0]).toContain('Online Payment')
  expect(rows[1]).toContain('Alipay')
  expect(rows[2]).toContain('custom_rail')
  expect(
    wrapper
      .findAll('[role="meter"]')
      .map((bar) => bar.attributes('aria-valuenow'))
  ).toEqual(['60', '30', '10'])
  wrapper.unmount()
})
