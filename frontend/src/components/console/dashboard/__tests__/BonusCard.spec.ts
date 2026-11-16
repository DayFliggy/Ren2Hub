import { mount, RouterLinkStub } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import BonusCard from '@/components/console/dashboard/BonusCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { TopupBonusSelf } from '@/types/topupBonus'
import { configureQuotaPerUnit } from '@/utils/quota'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(async () => {
  configureQuotaPerUnit(500_000)
  await setLocale('en')
})

const bonus: TopupBonusSelf = {
  status: 'ok',
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
const neutral: TopupBonusSelf = {
  status: 'inactive',
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
function render(data: TopupBonusSelf | null = bonus, extra = {}) {
  return mount(BonusCard, {
    props: { bonus: data, ...extra },
    global: { plugins: [i18n], stubs: { RouterLink: RouterLinkStub } },
  })
}

describe('BonusCard', () => {
  it('separates global components, the highest personal tier and the capped server result', () => {
    const wrapper = render()
    const global = wrapper.get('[data-bonus-global]')
    expect(global.text()).toContain('3% bonus')
    expect(global.get('[data-bonus-global-parts]').text()).toBe(
      'Fixed 1% · Site 2%'
    )
    const personal = wrapper.get('[data-bonus-personal]')
    expect(wrapper.get('[data-bonus-personal-rate]').text()).toContain(
      '4% bonus'
    )
    expect(personal.findAll('[data-bonus-window]')).toHaveLength(3)
    expect(
      personal.get('[data-selected="true"]').attributes('data-bonus-window')
    ).toBe('month')
    expect(personal.text()).not.toContain('Selected')
    expect(
      personal.get('[aria-current="true"]').attributes('data-bonus-window')
    ).toBe('month')
    const effective = wrapper.get('[data-bonus-effective]')
    // The headline is the capped server rate; the uncapped sum is only context.
    expect(effective.get('[data-bonus-rate]').text()).toBe('+5%')
    expect(effective.text()).toContain('Sum 7%')
    expect(wrapper.get('[data-bonus-cap]').text()).toBe('Capped at 5%')
    const ruler = wrapper.get('[data-bonus-ruler]')
    expect(ruler.findAll('.ruler-seg--global')).toHaveLength(3)
    expect(ruler.findAll('.ruler-seg--personal')).toHaveLength(2)
    expect(ruler.findAll('.ruler-seg--overflow')).toHaveLength(2)
  })
  it('follows the server winner when multiple windows have equal rates', () => {
    const wrapper = render({
      ...bonus,
      windows: bonus.windows.map((w) => ({ ...w, rate: '0.04' })),
    })
    expect(wrapper.findAll('[data-selected="true"]')).toHaveLength(1)
    expect(wrapper.findAll('[aria-current="true"]')).toHaveLength(1)
    expect(
      wrapper.get('[data-selected="true"]').attributes('data-bonus-window')
    ).toBe('month')
  })
  it('shows the uncapped component sum and leaves the cap to the server rate', () => {
    const wrapper = render({
      ...bonus,
      fixed_rate: '0.04',
      site_rate: '0.03',
      raw_rate: '0.11',
    })
    expect(wrapper.get('[data-bonus-global]').text()).toContain('7% bonus')
    expect(wrapper.get('[data-bonus-global-parts]').text()).toBe(
      'Fixed 4% · Site 3%'
    )
    expect(wrapper.get('[data-bonus-rate]').text()).toBe('+5%')
    expect(wrapper.get('[data-bonus-effective]').text()).toContain('Sum 11%')
    expect(wrapper.get('[data-bonus-cap]').text()).toBe('Capped at 5%')
  })
  it('does not mark an adopted window when every active window has zero bonus', () => {
    const wrapper = render({
      ...neutral,
      status: 'ok',
      windows: bonus.windows.map((window) => ({ ...window, rate: '0' })),
    })
    expect(wrapper.findAll('[data-bonus-window]')).toHaveLength(3)
    expect(wrapper.findAll('[data-selected="true"]')).toHaveLength(0)
    expect(wrapper.findAll('[aria-current]')).toHaveLength(0)
    expect(wrapper.get('[data-bonus-rate]').text()).toBe('0%')
    expect(wrapper.find('[data-bonus-empty]').exists()).toBe(false)
    expect(wrapper.find('[data-bonus-ruler]').exists()).toBe(false)
  })
  it.each(['en', 'zh-CN'])(
    'preserves the smallest supported bonus in %s',
    async (locale) => {
      await setLocale(locale)
      const wrapper = render({
        ...neutral,
        status: 'ok',
        fixed_rate: '0.0001',
        raw_rate: '0.0001',
        applied_rate: '0.0001',
        cap_rate: '0.05',
      })
      expect(wrapper.get('[data-bonus-global]').text()).toContain('0.01%')
      expect(wrapper.get('[data-bonus-rate]').text()).toBe('+0.01%')
      expect(
        wrapper.get('[data-bonus-ruler]').findAll('.ruler-seg--global')
      ).toHaveLength(1)
    }
  )
  it('shows counted usage and a higher-rate target without summing personal tiers', () => {
    const wrapper = render()
    const daily = wrapper.get('[data-bonus-progress="day"]')
    expect(daily.text()).toContain('5 USD')
    expect(daily.text()).toContain('10 USD to 2%')
    expect(wrapper.get('[data-bonus-progress="month"]').text()).toContain(
      'Max tier reached'
    )
  })
  it('compares large integer thresholds exactly and keeps a one-quota gap', () => {
    const wrapper = render({
      ...bonus,
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
    expect(wrapper.get('[data-bonus-progress="month"]').text()).toContain(
      '0.000002 USD to 2%'
    )
  })
  it('skips equal-rate thresholds and uses the configured quota unit', () => {
    configureQuotaPerUnit(100)
    const wrapper = render({
      ...bonus,
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
    expect(wrapper.get('[data-bonus-progress="day"]').text()).toContain(
      '2 USD to 2%'
    )
  })
  it.each(['inactive', 'ok'] as const)(
    'shows principal-only credit without invented progress or zero-value detail for %s',
    (status) => {
      const wrapper = render({ ...neutral, status, cap_rate: '0.05' })
      expect(wrapper.get('[data-bonus-empty]').text()).toContain(
        'Principal only'
      )
      expect(wrapper.get('[data-bonus-rate]').text()).toBe('0%')
      expect(wrapper.find('[data-bonus-ruler]').exists()).toBe(false)
      expect(wrapper.find('[data-bonus-global]').exists()).toBe(false)
      expect(wrapper.find('[data-bonus-window]').exists()).toBe(false)
      expect(wrapper.text()).not.toMatch(/shadow|excluded|not configured/i)
      expect(wrapper.find('[data-bonus-cap]').exists()).toBe(false)
    }
  )
  it('keeps reachable tiers at zero bonus and never calls an empty tier a maximum', () => {
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
    expect(wrapper.get('[data-bonus-progress="day"]').text()).toContain(
      '5 USD to 1%'
    )
    expect(wrapper.get('[data-bonus-progress="week"]').text()).toContain(
      'No bonus'
    )
    expect(wrapper.text()).not.toContain('Max tier reached')
    expect(wrapper.find('[data-bonus-empty]').exists()).toBe(false)
    expect(wrapper.find('[data-bonus-ruler]').exists()).toBe(false)
  })
  it('does not hide a zero cap that is suppressing an actual bonus', () => {
    const wrapper = render({
      ...neutral,
      fixed_rate: '0.01',
      raw_rate: '0.01',
      cap_applied: true,
    })
    expect(wrapper.find('[data-bonus-empty]').exists()).toBe(false)
    expect(wrapper.get('[data-bonus-global-parts]').text()).toContain(
      'Fixed 1%'
    )
    expect(wrapper.get('[data-bonus-cap]').text()).toContain('0%')
    expect(wrapper.get('[data-bonus-rate]').text()).toBe('0%')
  })
  it('preserves global bonus when no personal window applies', () => {
    const wrapper = render({
      ...neutral,
      status: 'ok',
      fixed_rate: '0.01',
      raw_rate: '0.01',
      applied_rate: '0.01',
      cap_rate: '0.05',
    })
    expect(wrapper.find('[data-bonus-empty]').exists()).toBe(false)
    expect(wrapper.get('[data-bonus-personal-empty]').text()).toBe(
      'No personal tier bonus currently applies.'
    )
    expect(wrapper.get('[data-bonus-rate]').text()).toBe('+1%')
  })
  it('lists amount-dependent tiers and never calls them principal-only', () => {
    const wrapper = render({
      ...neutral,
      status: 'ok',
      cap_rate: '0.05',
      amount_tiers: [
        { threshold_quota: '50000000', rate: '0.02' },
        { threshold_quota: '250000000', rate: '0.04' },
      ],
    })
    expect(wrapper.find('[data-bonus-empty]').exists()).toBe(false)
    expect(wrapper.get('[data-bonus-amount-tiers]').text()).toBe(
      'Larger credits also get: 2% bonus from 100 USD · 4% bonus from 500 USD'
    )
    expect(wrapper.get('[data-bonus-rate]').text()).toBe('0%')
  })
  it('never invents a rate when the request fails or has not loaded', () => {
    const failed = render(null, { error: 'Request timed out' })
    expect(failed.get('[data-bonus-error]').text()).toContain(
      'Request timed out'
    )
    expect(failed.find('[data-bonus-effective]').exists()).toBe(false)
    const pending = render(null, { loading: true })
    expect(pending.find('[aria-busy="true"]').exists()).toBe(true)
    expect(pending.text()).not.toContain('%')
    expect(render(null).find('[data-bonus-not-connected]').exists()).toBe(true)
  })
  it('exposes a labelled settings link only when permitted and removes it on permission loss', async () => {
    const wrapper = render(bonus, { canConfigure: true })
    expect(wrapper.getComponent(RouterLinkStub).props('to')).toBe(
      '/system-settings/billing/topup-bonus'
    )
    expect(wrapper.get('[data-bonus-configure]').attributes('aria-label')).toBe(
      'Configure top-up bonuses'
    )
    await wrapper.setProps({ canConfigure: false })
    expect(wrapper.find('[data-bonus-configure]').exists()).toBe(false)
  })
})
