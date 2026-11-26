import { mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import BalanceCard from '@/components/console/dashboard/BalanceCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  localStorage.clear()
})

describe('BalanceCard', () => {
  it('does not relabel today spend as monthly spend', () => {
    const wrapper = mount(BalanceCard, {
      props: { quota: '5000000', todayQuota: '500000' },
      global: { plugins: [i18n] },
    })

    const monthTile = wrapper.findAll('[data-balance-spend-tile]')[0]!
    expect(monthTile.text()).toContain("This month's spend")
    expect(monthTile.text()).toContain('--')
    expect(monthTile.text()).not.toContain('$1.00')
  })

  it('renders the supplied calendar-month spend', () => {
    const wrapper = mount(BalanceCard, {
      props: { quota: '5000000', monthQuota: '1500000' },
      global: { plugins: [i18n] },
    })

    expect(wrapper.findAll('[data-balance-spend-tile]')[0]!.text()).toContain(
      '$3.00'
    )
  })

  it('does not turn a missing balance into a zero balance', () => {
    const wrapper = mount(BalanceCard, {
      props: { quota: null },
      global: { plugins: [i18n] },
    })

    expect(wrapper.text()).toContain('--')
    expect(wrapper.text()).not.toContain('$0.00')
    expect(wrapper.text()).toContain('UNKNOWN')
  })

  it('renders valid account fields whose display denominator exceeds int64', () => {
    const wrapper = mount(BalanceCard, {
      props: {
        quota: '9223372036854775807',
        usedQuota: '9223372036854775807',
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.text()).toContain('$18,446,744,073,709.55')
    expect(wrapper.text()).toContain('50%')
    wrapper.unmount()
  })

  it('uses display-number monospace styling on spend tiles and monospace on used quota', () => {
    const wrapper = mount(BalanceCard, {
      props: {
        quota: '100000000',
        usedQuota: '6250000',
        monthQuota: '7240000',
        dailyBurn: '955000',
      },
      global: { plugins: [i18n] },
    })

    const tiles = wrapper.findAll('[data-balance-spend-tile]')
    expect(tiles).toHaveLength(2)
    for (const tile of tiles) {
      const value = tile.find('.display-number')
      expect(value.exists()).toBe(true)
      expect(value.classes()).toContain('font-mono')
      expect(value.classes()).not.toContain('font-bold')
    }
    const meter = wrapper.get('[data-balance-usage-meter]')
    expect(meter.find('.font-mono.tabular-nums').text()).toContain('$12.50')
    wrapper.unmount()
  })
})
