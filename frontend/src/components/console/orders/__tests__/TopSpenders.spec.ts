import { mount } from '@vue/test-utils'
import { beforeAll, expect, it } from 'vitest'

import TopSpenders from '@/components/console/orders/TopSpenders.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

function render(props: InstanceType<typeof TopSpenders>['$props']) {
  return mount(TopSpenders, { props, global: { plugins: [i18n] } })
}

const noRevenue = () => i18n.global.t('orders.noRevenue')

it('shows only the failure state when the ranking request failed', () => {
  const wrapper = render({
    items: [],
    unavailable: true,
    error: 'ranking failed',
  })
  expect(wrapper.text()).toContain('ranking failed')
  expect(wrapper.text()).not.toContain(noRevenue())
  wrapper.unmount()
})

it('shows only the skeleton while loading', () => {
  const wrapper = render({ items: [], loading: true })
  expect(wrapper.find('ul[aria-hidden="true"]').exists()).toBe(true)
  expect(wrapper.text()).not.toContain(noRevenue())
  wrapper.unmount()
})

it('shows the ranking with its scope note, or the empty state without data', () => {
  const ranked = render({
    items: [
      {
        user_id: 7,
        username: 'alice',
        paid_count: 2,
        credited_quota: '500000',
      },
    ],
  })
  expect(ranked.findAll('ol li')).toHaveLength(1)
  expect(ranked.text()).toContain(i18n.global.t('orders.topSpendersScope'))
  expect(ranked.text()).not.toContain(noRevenue())
  ranked.unmount()
  const empty = render({ items: [] })
  expect(empty.text()).toContain(noRevenue())
  expect(empty.text()).not.toContain(i18n.global.t('orders.topSpendersScope'))
  empty.unmount()
})
