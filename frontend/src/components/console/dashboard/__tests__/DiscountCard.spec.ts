import { mount, RouterLinkStub } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import DiscountCard from '@/components/console/dashboard/DiscountCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { TopupDiscountSelf } from '@/types/topupDiscount'
import { configureQuotaPerUnit } from '@/utils/quota'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(async () => {
  configureQuotaPerUnit(500_000)
  await setLocale('en')
})

const discounts: TopupDiscountSelf = {
  status: 'ok',
  factor: '0.95',
  fixed_rate: '0.01',
  site_rate: '0.02',
  personal_rate: '0.04',
  raw_rate: '0.07',
  applied_rate: '0.05',
  cap_rate: '0.05',
  cap_applied: true,
  winner_window: 'month',
  business_date: '2026-11-02',
  amount_tiers: [],
  windows: [
    {
      window: 'day',
      quota: '2500000',
      rate: '0.01',
      tiers: [
        { threshold_quota: '2500000', rate: '0.01' },
        { threshold_quota: '7500000', rate: '0.02' },
      ],
    },
    { window: 'week', quota: '62500000', rate: '0.03', tiers: [] },
    { window: 'month', quota: '375000000', rate: '0.04', tiers: [] },
  ],
}
const neutral: TopupDiscountSelf = {
  status: 'inactive',
  factor: '1',
  fixed_rate: '0',
  site_rate: '0',
  personal_rate: '0',
  raw_rate: '0',
  applied_rate: '0',
  cap_rate: '0',
  cap_applied: false,
  amount_tiers: [],
  windows: [],
}
function render(data: TopupDiscountSelf | null = discounts, extra = {}) {
  return mount(DiscountCard, {
    props: { discounts: data, ...extra },
    global: { plugins: [i18n], stubs: { RouterLink: RouterLinkStub } },
  })
}

describe('DiscountCard', () => {
  it('separates global components, the highest personal tier and the capped server result', () => {
    const wrapper = render()
    const global = wrapper.get('[data-discount-global]')
    expect(global.text()).toContain('3% off')
    expect(global.get('[data-discount-global-parts]').text()).toBe(
      'Fixed 1% · Site 2%'
    )
    const personal = wrapper.get('[data-discount-personal]')
    expect(wrapper.get('[data-discount-personal-rate]').text()).toContain(
      '4% off'
    )
    expect(personal.findAll('[data-discount-window]')).toHaveLength(3)
    expect(
      personal.get('[data-selected="true"]').attributes('data-discount-window')
    ).toBe('month')
    expect(personal.text()).not.toContain('Selected')
    expect(
      personal.get('[aria-current="true"]').attributes('data-discount-window')
    ).toBe('month')
    const effective = wrapper.get('[data-discount-effective]')
    expect(effective.text()).toContain('0.95×')
    expect(effective.text()).toContain('5% off')
    expect(effective.text()).not.toContain('7% off')
    expect(wrapper.get('[data-discount-cap]').text()).toBe('Capped at 5%')
    const ruler = wrapper.get('[data-discount-ruler]')
    expect(ruler.findAll('.ruler-seg--global')).toHaveLength(3)
    expect(ruler.findAll('.ruler-seg--personal')).toHaveLength(2)
    expect(ruler.findAll('.ruler-seg--overflow')).toHaveLength(2)
  })
  it('follows the server winner when multiple windows have equal rates', () => {
    const wrapper = render({
      ...discounts,
      windows: discounts.windows.map((w) => ({ ...w, rate: '0.04' })),
    })
    expect(wrapper.findAll('[data-selected="true"]')).toHaveLength(1)
    expect(wrapper.findAll('[aria-current="true"]')).toHaveLength(1)
    expect(
      wrapper.get('[data-selected="true"]').attributes('data-discount-window')
    ).toBe('month')
  })
  it('shows the uncapped component sum and leaves the cap to the server factor', () => {
    const wrapper = render({
      ...discounts,
      fixed_rate: '0.04',
      site_rate: '0.03',
      raw_rate: '0.11',
    })
    expect(wrapper.get('[data-discount-global]').text()).toContain('7% off')
    expect(wrapper.get('[data-discount-global-parts]').text()).toBe(
      'Fixed 4% · Site 3%'
    )
    expect(wrapper.get('[data-discount-effective]').text()).toContain('0.95×')
    expect(wrapper.get('[data-discount-effective]').text()).toContain('Sum 11%')
    expect(wrapper.get('[data-discount-cap]').text()).toBe('Capped at 5%')
  })
  it('does not mark an adopted window when every active window has zero savings', () => {
    const wrapper = render({
      ...neutral,
      status: 'ok',
      windows: discounts.windows.map((window) => ({ ...window, rate: '0' })),
    })
    expect(wrapper.findAll('[data-discount-window]')).toHaveLength(3)
    expect(wrapper.findAll('[data-selected="true"]')).toHaveLength(0)
    expect(wrapper.findAll('[aria-current]')).toHaveLength(0)
    expect(wrapper.get('[data-discount-effective]').text()).toContain('1×')
    expect(wrapper.find('[data-discount-empty]').exists()).toBe(false)
    expect(wrapper.find('[data-discount-ruler]').exists()).toBe(false)
  })
  it.each(['en', 'zh-CN'])(
    'preserves the smallest supported discount in %s',
    async (locale) => {
      await setLocale(locale)
      const wrapper = render({
        ...neutral,
        status: 'ok',
        fixed_rate: '0.0001',
        raw_rate: '0.0001',
        factor: '0.9999',
        applied_rate: '0.0001',
        cap_rate: '0.05',
      })
      expect(wrapper.get('[data-discount-global]').text()).toContain('0.01%')
      expect(wrapper.get('[data-discount-effective]').text()).toContain(
        '0.9999×'
      )
      expect(wrapper.get('[data-discount-effective]').text()).toContain('0.01%')
      expect(
        wrapper.get('[data-discount-ruler]').findAll('.ruler-seg--global')
      ).toHaveLength(1)
      expect(wrapper.text()).not.toContain('10 折')
    }
  )
  it('shows counted usage and a higher-rate target without summing personal tiers', () => {
    const wrapper = render()
    const daily = wrapper.get('[data-discount-progress="day"]')
    expect(daily.text()).toContain('5 USD')
    expect(daily.text()).toContain('10 USD to 2%')
    expect(wrapper.get('[data-discount-progress="month"]').text()).toContain(
      'Max tier reached'
    )
  })
  it('compares large integer thresholds exactly and keeps a one-quota gap', () => {
    const wrapper = render({
      ...discounts,
      winner_window: 'month',
      windows: [
        {
          window: 'month',
          quota: '999999999999999998',
          rate: '0.01',
          tiers: [{ threshold_quota: '999999999999999999', rate: '0.02' }],
        },
      ],
    })
    expect(wrapper.get('[data-discount-progress="month"]').text()).toContain(
      '0.000002 USD to 2%'
    )
  })
  it('skips equal-rate thresholds and uses the configured quota unit', () => {
    configureQuotaPerUnit(100)
    const wrapper = render({
      ...discounts,
      winner_window: 'day',
      windows: [
        {
          window: 'day',
          quota: '100',
          rate: '0.01',
          tiers: [
            { threshold_quota: '200', rate: '0.01' },
            { threshold_quota: '300', rate: '0.02' },
          ],
        },
      ],
    })
    expect(wrapper.get('[data-discount-progress="day"]').text()).toContain(
      '2 USD to 2%'
    )
  })
  it.each(['inactive', 'ok'] as const)(
    'shows standard pricing without invented progress or zero-value detail for %s',
    (status) => {
      const wrapper = render({ ...neutral, status, cap_rate: '0.05' })
      expect(wrapper.get('[data-discount-empty]').text()).toContain(
        'Full price'
      )
      expect(wrapper.get('[data-discount-effective]').text()).toContain('1×')
      expect(wrapper.find('[data-discount-ruler]').exists()).toBe(false)
      expect(wrapper.find('[data-discount-global]').exists()).toBe(false)
      expect(wrapper.find('[data-discount-window]').exists()).toBe(false)
      expect(wrapper.text()).not.toMatch(/shadow|excluded|not configured/i)
      expect(wrapper.find('[data-discount-cap]').exists()).toBe(false)
    }
  )
  it('keeps reachable tiers at zero savings and never calls an empty tier a maximum', () => {
    const wrapper = render({
      ...neutral,
      status: 'ok',
      windows: [
        {
          window: 'day',
          quota: '0',
          rate: '0',
          tiers: [{ threshold_quota: '2500000', rate: '0.01' }],
        },
        { window: 'week', quota: '0', rate: '0', tiers: [] },
      ],
    })
    expect(wrapper.get('[data-discount-progress="day"]').text()).toContain(
      '5 USD to 1%'
    )
    expect(wrapper.get('[data-discount-progress="week"]').text()).toContain(
      'No discount'
    )
    expect(wrapper.text()).not.toContain('Max tier reached')
    expect(wrapper.find('[data-discount-empty]').exists()).toBe(false)
    expect(wrapper.find('[data-discount-ruler]').exists()).toBe(false)
  })
  it('does not hide a zero cap that is suppressing actual savings', () => {
    const wrapper = render({
      ...neutral,
      fixed_rate: '0.01',
      raw_rate: '0.01',
      cap_applied: true,
    })
    expect(wrapper.find('[data-discount-empty]').exists()).toBe(false)
    expect(wrapper.get('[data-discount-global-parts]').text()).toContain(
      'Fixed 1%'
    )
    expect(wrapper.get('[data-discount-cap]').text()).toContain('0%')
    expect(wrapper.get('[data-discount-factor]').text()).toBe('1×')
  })
  it('preserves global savings when no personal window applies', () => {
    const wrapper = render({
      ...neutral,
      status: 'ok',
      fixed_rate: '0.01',
      raw_rate: '0.01',
      factor: '0.99',
      applied_rate: '0.01',
      cap_rate: '0.05',
    })
    expect(wrapper.find('[data-discount-empty]').exists()).toBe(false)
    expect(wrapper.get('[data-discount-personal-empty]').text()).toBe(
      'No personal tier discount currently applies.'
    )
    expect(wrapper.get('[data-discount-factor]').text()).toBe('0.99×')
  })
  it('lists order-dependent amount tiers and never calls them standard pricing', () => {
    const wrapper = render({
      ...neutral,
      status: 'ok',
      cap_rate: '0.05',
      amount_tiers: [
        { threshold_quota: '50000000', rate: '0.02' },
        { threshold_quota: '250000000', rate: '0.04' },
      ],
    })
    expect(wrapper.find('[data-discount-empty]').exists()).toBe(false)
    expect(wrapper.get('[data-discount-amount-tiers]').text()).toBe(
      'Larger top-ups also get: 2% off from 100 USD · 4% off from 500 USD'
    )
    expect(wrapper.get('[data-discount-factor]').text()).toBe('1×')
  })
  it('never invents a rate when the request fails or has not loaded', () => {
    const failed = render(null, { error: 'Request timed out' })
    expect(failed.get('[data-discount-error]').text()).toContain(
      'Request timed out'
    )
    expect(failed.find('[data-discount-effective]').exists()).toBe(false)
    const pending = render(null, { loading: true })
    expect(pending.find('[aria-busy="true"]').exists()).toBe(true)
    expect(pending.text()).not.toContain('×')
    expect(render(null).find('[data-discount-not-connected]').exists()).toBe(
      true
    )
  })
  it('exposes a labelled settings link only when permitted and removes it on permission loss', async () => {
    const wrapper = render(discounts, { canConfigure: true })
    expect(wrapper.getComponent(RouterLinkStub).props('to')).toBe(
      '/system-settings/billing/topup-discount'
    )
    expect(
      wrapper.get('[data-discount-configure]').attributes('aria-label')
    ).toBe('Configure top-up discounts')
    await wrapper.setProps({ canConfigure: false })
    expect(wrapper.find('[data-discount-configure]').exists()).toBe(false)
  })
})
