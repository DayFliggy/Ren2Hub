import { mount, RouterLinkStub } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import DiscountCard from '@/components/console/dashboard/DiscountCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { UsageDiscountSelf } from '@/types/usageDiscount'
import { configureQuotaPerUnit } from '@/utils/quota'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(async () => {
  configureQuotaPerUnit(500_000)
  await setLocale('en')
})

const discounts: UsageDiscountSelf = {
  status: 'ok',
  factor: '0.95',
  global_factor: '0.97',
  fixed_rate: '0.01',
  site_rate: '0.02',
  applied_rate: '0.05',
  cap_rate: '0.05',
  cap_applied: true,
  winner_window: 'month',
  business_date: '2026-11-02',
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
const neutral: UsageDiscountSelf = {
  status: 'inactive',
  factor: '1',
  global_factor: '1',
  fixed_rate: '0',
  site_rate: '0',
  applied_rate: '0',
  cap_rate: '0',
  cap_applied: false,
  windows: [],
}
function render(data: UsageDiscountSelf | null = discounts, extra = {}) {
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
  it('keeps capped global savings distinct from the uncapped components', () => {
    const wrapper = render({
      ...discounts,
      fixed_rate: '0.04',
      site_rate: '0.03',
      global_factor: '0.95',
    })
    expect(wrapper.get('[data-discount-global]').text()).toContain('5% off')
    expect(wrapper.get('[data-discount-global-parts]').text()).toBe(
      'Fixed 4% · Site 3%'
    )
    expect(wrapper.get('[data-discount-effective]').text()).toContain('0.95×')
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
        factor: '0.9999',
        global_factor: '0.9999',
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
  it.each(['inactive', 'ok', 'not_applicable'] as const)(
    'shows standard pricing without invented progress or zero-value detail for %s',
    (status) => {
      const wrapper = render({ ...neutral, status, cap_rate: '0.05' })
      expect(wrapper.get('[data-discount-empty]').text()).toContain(
        'Standard pricing'
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
      global_factor: '0.99',
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
  it('never treats unavailable data without a fixed discount as normal standard pricing', () => {
    const wrapper = render({ ...neutral, status: 'unavailable' })
    expect(wrapper.find('[data-discount-empty]').exists()).toBe(false)
    expect(wrapper.get('[data-discount-unavailable]').text()).toContain(
      'temporarily unavailable'
    )
    expect(wrapper.get('[data-discount-factor]').text()).toBe('1×')
  })
  it('distinguishes unavailable windows from a zero personal discount', () => {
    const wrapper = render({
      ...neutral,
      status: 'unavailable',
      factor: '0.99',
      global_factor: '0.99',
      fixed_rate: '0.01',
      applied_rate: '0.01',
      cap_rate: '0.05',
    })
    expect(wrapper.get('[data-discount-unavailable]').text()).toContain(
      'only the fixed discount applies'
    )
    expect(wrapper.get('[data-discount-personal-empty]').text()).toContain(
      'temporarily unavailable'
    )
    expect(wrapper.get('[data-discount-personal-rate]').text()).toBe('—')
    expect(wrapper.get('[data-discount-effective]').text()).toContain('0.99×')
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
      '/system-settings/billing/usage-discount'
    )
    expect(
      wrapper.get('[data-discount-configure]').attributes('aria-label')
    ).toBe('Configure usage discounts')
    await wrapper.setProps({ canConfigure: false })
    expect(wrapper.find('[data-discount-configure]').exists()).toBe(false)
  })
})
