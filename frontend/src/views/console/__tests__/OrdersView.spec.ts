import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { expect, it, vi } from 'vitest'
import messages from '@/i18n/locales/en/console'
import type { AdminOrderStats } from '@/types/console'
const mocks = vi.hoisted(() => ({ orders: vi.fn() }))
vi.mock('@/composables/useAdminOrders', () => ({
  useAdminOrders: mocks.orders,
  ORDER_EXPORT_MAX_ROWS: 10000,
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({}) }))
import OrdersView from '../OrdersView.vue'
import OrderStatCard from '@/components/console/orders/OrderStatCard.vue'

function render(stats: AdminOrderStats, staleError = '') {
  mocks.orders.mockReturnValue({
    rows: ref([]),
    total: ref(0),
    page: ref(1),
    pageSize: ref(20),
    keyword: ref(''),
    status: ref(''),
    method: ref(''),
    loading: ref(false),
    refreshing: ref(false),
    initialError: ref(''),
    stats: ref(stats),
    range: ref(30),
    statsLoading: ref(false),
    statsRefreshing: ref(false),
    statsError: ref(staleError),
    statsStale: ref(Boolean(staleError)),
    load: vi.fn(),
    loadStats: vi.fn(),
    fetchAllForExport: vi.fn(),
    refreshAll: vi.fn(),
  })
  return mount(OrdersView, {
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: messages } }),
      ],
      stubs: {
        OrderRevenueChart: {
          name: 'OrderRevenueChart',
          props: ['points', 'currency', 'partialAmounts'],
          template:
            '<div data-chart>{{ currency }} {{ JSON.stringify(points) }}</div>',
        },
        OrderDetailModal: true,
      },
    },
  })
}
const cny = {
  currency: 'CNY',
  count: 3,
  paid_count: 2,
  paid_amount: '73',
  credited_quota: 500,
  unknown_amount_count: 0,
}
const usd = {
  ...cny,
  currency: 'USD',
  count: 8,
  paid_count: 4,
  paid_amount: '8',
}
it('restores four metrics and the chart without adding amounts across currencies', async () => {
  const wrapper = render({
    total: 11,
    currencies: [cny, usd],
    daily: [
      { ...cny, date: '2026-10-19' },
      { ...usd, date: '2026-10-19' },
    ],
    start_time: 1,
    end_time: 2,
  })
  const values = () =>
    wrapper.findAllComponents(OrderStatCard).map((card) => card.props('value'))
  expect(values()).toEqual(['¥73.00', '3', '2', '¥36.50'])
  expect(wrapper.get('[data-chart]').text()).toContain('"revenue":73')
  expect(wrapper.get('[data-chart]').text()).not.toContain('"revenue":8')
  const currency = wrapper
    .findAllComponents({ name: 'FilterSelect' })
    .find((select) => select.props('label') === 'Currency')!
  await currency.vm.$emit('update:modelValue', 'USD')
  await wrapper.vm.$nextTick()
  expect(values()).toEqual(['$8.00', '8', '4', '$2.00'])
  expect(wrapper.get('[data-chart]').text()).toContain('"revenue":8')
  expect(wrapper.get('[data-chart]').text()).not.toContain('"revenue":73')
  expect(wrapper.text()).toContain('This statistic is currently unavailable.')
  wrapper.unmount()
})
it('does not invent zero amounts or an average for unrecorded currencies', () => {
  const wrapper = render({
    total: 1,
    currencies: [
      { ...cny, currency: '', paid_amount: '0', unknown_amount_count: 1 },
    ],
    daily: [],
    start_time: 1,
    end_time: 2,
  })
  expect(
    wrapper.findAllComponents(OrderStatCard).map((card) => card.props('value'))
  ).toEqual(['—', '—', '—', '—'])
  expect(wrapper.get('[role="status"]').text()).toContain('1')
  expect(wrapper.get('[data-chart]').text()).toContain('[]')
  wrapper.unmount()
})

it('keeps known amounts as a subtotal and withholds an average when amounts are missing', () => {
  const wrapper = render({
    total: 3,
    currencies: [{ ...cny, unknown_amount_count: 1 }],
    daily: [],
    start_time: 1,
    end_time: 2,
  })
  const cards = wrapper.findAllComponents(OrderStatCard)
  expect(cards.map((card) => card.props('value'))).toEqual([
    '¥73.00',
    '3',
    '2',
    '—',
  ])
  expect(
    wrapper.findComponent({ name: 'OrderRevenueChart' }).props('partialAmounts')
  ).toBe(true)
  expect(cards[0]!.props('hint')).toBe('Known amounts subtotal')
  expect(cards[3]!.props('hint')).toContain('amounts are missing')
  wrapper.unmount()
})

it('labels a retained snapshot after a refresh failure', () => {
  const wrapper = render(
    {
      total: 3,
      currencies: [cny],
      daily: [],
      start_time: 1,
      end_time: 2,
    },
    'Refresh unavailable'
  )
  expect(wrapper.get('[role="alert"]').text()).toContain(
    'last successful snapshot'
  )
  expect(wrapper.get('[role="alert"]').text()).toContain('Refresh unavailable')
  expect(wrapper.findAllComponents(OrderStatCard)[0]!.props('value')).toBe(
    '¥73.00'
  )
  wrapper.unmount()
})
