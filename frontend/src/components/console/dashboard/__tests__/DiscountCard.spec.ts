import { mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'

import DiscountCard from '@/components/console/dashboard/DiscountCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { UserDiscounts } from '@/composables/useDashboard'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

afterEach(async () => {
  await setLocale('en')
})

/** Platform and effective differ, so each figure can be traced to its ratio. */
const discounts: UserDiscounts = {
  status: 'ok',
  global_ratio: 0.9,
  effective_ratio: 0.72,
}

function render(props: Record<string, unknown> = {}) {
  return mount(DiscountCard, {
    props: { discounts, ...props },
    global: { plugins: [i18n] },
  })
}

/** The tile whose label reads `label`, as label/value/sub text. */
function tile(wrapper: ReturnType<typeof render>, label: string) {
  const match = wrapper.findAll('p').find((p) => p.text() === label)
    ?.element.parentElement
  if (!match) throw new Error(`no tile labelled ${label}`)
  return match.textContent ?? ''
}

describe('DiscountCard', () => {
  it('shows a not-connected state inside the card when discounts are null', () => {
    const wrapper = render({ discounts: null })
    const state = wrapper.find('[data-discount-not-connected]')

    expect(state.exists()).toBe(true)
    // Contained in the card body rather than floating on the page.
    expect(
      wrapper.find('[data-discount-card]').element.contains(state.element)
    ).toBe(true)
    expect(state.text()).toContain('Not connected')
    // No fabricated rate, bar or tiles.
    expect(wrapper.text()).not.toContain('×')
    expect(wrapper.find('[data-discount-effective-bar]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Global discount')
    expect(wrapper.text()).not.toContain('At list price')
  })

  it('reads 未接入 in Chinese when discounts are null', async () => {
    await setLocale('zh-CN')
    const wrapper = render({ discounts: null })

    expect(wrapper.find('[data-discount-not-connected]').text()).toContain(
      '未接入'
    )
    expect(wrapper.text()).not.toContain('按原价计费')
  })

  it('renders the platform discount without a plan discount', () => {
    const wrapper = render()

    expect(wrapper.text()).not.toContain('Personal discount')
  })

  it('headlines the effective rate and draws its bar from that same rate', () => {
    const wrapper = render()
    const bar = wrapper.find('[data-discount-effective-bar]')

    expect(wrapper.text()).toContain('0.720×')
    // 28% off, scaled so 20% off fills the track.
    expect(bar.attributes('aria-label')).toBe('28% off')
    expect((bar.find('div').element as HTMLElement).style.width).toBe('100%')
  })

  it('keeps the platform ratio and the effective depth in separate tiles', () => {
    const wrapper = render()

    const platform = tile(wrapper, 'Global discount')
    expect(platform).toContain('0.90×')
    expect(platform).toContain('Platform 10% off')
    expect(platform).not.toContain('28%')

    const effective = tile(wrapper, 'Effective discount')
    expect(effective).toContain('28% off')
    expect(effective).not.toContain('10%')
  })

  it('writes Chinese discounts in 折, not as the percentage off', async () => {
    await setLocale('zh-CN')
    const wrapper = render({
      discounts: { status: 'ok', global_ratio: 0.88, effective_ratio: 0.88 },
    })

    expect(tile(wrapper, '全局折扣')).toContain('平台 8.8 折')
    expect(tile(wrapper, '综合折扣')).toContain('8.8 折')
    expect(wrapper.text()).not.toContain('12折')
    expect(wrapper.text()).not.toContain('12 折')
  })

  it.each([
    {
      locale: 'en',
      globalCopy: 'No platform discount',
      effectiveCopy: 'At list price',
      forbidden: ['0% off'],
    },
    {
      locale: 'zh-CN',
      globalCopy: '平台暂无折扣',
      effectiveCopy: '按原价计费',
      forbidden: ['0 折', '10 折'],
    },
  ])(
    'says there is no discount instead of a zero discount in $locale',
    async ({ locale, globalCopy, effectiveCopy, forbidden }) => {
      await setLocale(locale)
      const wrapper = render({
        discounts: { status: 'inactive', global_ratio: 1, effective_ratio: 1 },
      })

      expect(wrapper.text()).toContain(globalCopy)
      expect(wrapper.text()).toContain(effectiveCopy)
      for (const copy of forbidden) expect(wrapper.text()).not.toContain(copy)
      const bar = wrapper.find('[data-discount-effective-bar] div')
      expect((bar.element as HTMLElement).style.width).toBe('0%')
      expect(wrapper.find('[data-discount-unavailable]').exists()).toBe(false)
    }
  )

  it.each([
    { locale: 'en', note: 'Usage discount is temporarily unavailable' },
    { locale: 'zh-CN', note: '用量折扣暂不可用' },
  ])(
    'keeps the fixed discount when usage data is unavailable in $locale',
    async ({ locale, note }) => {
      await setLocale(locale)
      // Only the 1% fixed part applies while the usage windows are unreadable.
      const wrapper = render({
        discounts: {
          status: 'unavailable',
          global_ratio: 0.99,
          effective_ratio: 0.99,
        },
      })

      expect(wrapper.get('[data-discount-unavailable]').text()).toContain(note)
      expect(wrapper.text()).toContain('0.990×')
      expect(wrapper.text()).toContain('0.99×')
      expect(wrapper.find('[data-discount-not-connected]').exists()).toBe(false)
    }
  )

  it('says the discount could not be read instead of inventing a rate', () => {
    const wrapper = render({ discounts: null, error: 'Request timed out' })
    const state = wrapper.get('[data-discount-error]')

    expect(state.text()).toContain('Discount unavailable')
    expect(state.text()).toContain('Request timed out')
    expect(wrapper.text()).not.toContain('×')
    expect(wrapper.text()).not.toContain('Not connected')
  })

  it('shows the loading skeleton before the discount arrives', () => {
    const wrapper = render({ discounts: null, loading: true })

    expect(wrapper.find('[data-discount-not-connected]').exists()).toBe(false)
    expect(wrapper.find('[data-discount-error]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('×')
  })
})
