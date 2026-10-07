import { mount } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import UsageDistributionCard from '@/components/console/dashboard/UsageDistributionCard.vue'
import type { UsageDistributionPoint } from '@/composables/useUsageDistribution'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

function dateKey(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function points(): UsageDistributionPoint[] {
  const end = new Date(2026, 6, 27)
  return Array.from({ length: 365 }, (_, index) => {
    const date = new Date(end)
    date.setDate(date.getDate() - (364 - index))
    return {
      date: dateKey(date),
      requests: index + 1,
      consume: String((index + 1) * 500_000),
      tokens: (index + 1) * 1_000,
    }
  })
}

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('zh-CN')
})

beforeEach(async () => {
  await setLocale('zh-CN')
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 6, 27, 12))
})

afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

function render(overrides: Record<string, unknown> = {}) {
  return mount(UsageDistributionCard, {
    props: { points: points(), ...overrides },
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
}

type Wrapper = ReturnType<typeof render>

function radio(wrapper: Wrapper, group: string, label: string) {
  const option = wrapper
    .get(`[role="radiogroup"][aria-label="${group}"]`)
    .findAll('[role="radio"]')
    .find((entry) => entry.text() === label)
  if (!option) throw new Error(`missing ${group} option ${label}`)
  return option
}

const period = (wrapper: Wrapper, label: string) =>
  radio(wrapper, '统计周期', label)
const metric = (wrapper: Wrapper, label: string) =>
  radio(wrapper, '分布指标', label)

describe('UsageDistributionCard', () => {
  it('defaults to month and calls with one roving grid focus', () => {
    const wrapper = render()

    expect(period(wrapper, '月').attributes('aria-checked')).toBe('true')
    expect(metric(wrapper, '调用').attributes('aria-checked')).toBe('true')
    expect(
      wrapper.findAll('[aria-label="统计周期"] [role="radio"]')
    ).toHaveLength(3)
    expect(wrapper.findAll('[data-usage-date]')).toHaveLength(30)
    expect(wrapper.findAll('[data-usage-placeholder]')).toHaveLength(12)
    expect(wrapper.find('[data-usage-layout="month"]').exists()).toBe(true)
    expect(wrapper.get('[data-usage-footer]').text()).toContain('活跃 30 天')
    expect(
      wrapper
        .findAll('[data-usage-date]')
        .filter((cell) => cell.attributes('tabindex') === '0')
    ).toHaveLength(1)
  })

  it('keeps six calendar weeks when the month spans only five', () => {
    vi.setSystemTime(new Date(2026, 6, 26, 12))
    const wrapper = render()

    expect(wrapper.findAll('[data-usage-date]')).toHaveLength(30)
    const placeholders = wrapper.findAll('[data-usage-placeholder]')
    expect(placeholders).toHaveLength(12)
    expect(placeholders.at(-1)!.attributes('style')).toContain('grid-row: 6')
  })

  it('formats fractional spend in the total, cells and analytics', async () => {
    const wrapper = render({
      points: [
        { date: '2026-07-27', requests: 1, consume: '1480000', tokens: 1 },
      ],
    })
    await metric(wrapper, '消费').trigger('click')

    expect(wrapper.get('[data-usage-total]').text()).toBe('$2.96')
    expect(
      wrapper.get('[data-usage-date="2026-07-27"]').attributes('aria-label')
    ).toContain('$2.96')
    expect(wrapper.get('[data-usage-segment-bar="ranked"]').text()).toContain(
      '$2.96'
    )
  })

  it('switches period and metric without replacing the card', async () => {
    const wrapper = render()
    const original = wrapper.get('[data-usage-distribution]').element

    await period(wrapper, '季').trigger('click')
    await metric(wrapper, '消费').trigger('click')

    expect(wrapper.get('[data-usage-distribution]').element).toBe(original)
    expect(wrapper.findAll('[data-usage-date]')).toHaveLength(91)
    expect(wrapper.find('[data-usage-layout="quarter"]').exists()).toBe(true)
    expect(wrapper.get('[data-usage-content]').classes()).toContain('grow')
    expect(wrapper.get('[data-usage-total]').text()).toMatch(/^\$/)
  })

  it('uses side analytics for quarter and bottom analytics for year', async () => {
    const wrapper = render()
    await period(wrapper, '季').trigger('click')

    expect(wrapper.findAll('[data-usage-date]')).toHaveLength(91)
    expect(wrapper.find('[data-usage-layout="quarter"]').exists()).toBe(true)
    expect(wrapper.find('[data-usage-analytics="side"]').exists()).toBe(true)
    expect(wrapper.get('[data-usage-analytics="side"]').text()).toContain(
      '本段最活跃'
    )
    expect(wrapper.get('[data-usage-analytics="side"]').text()).toContain(
      '星期节律'
    )

    await period(wrapper, '年').trigger('click')

    expect(wrapper.findAll('[data-usage-date]')).toHaveLength(364)
    expect(wrapper.find('[data-usage-layout="year"]').exists()).toBe(true)
    expect(wrapper.get('[data-usage-content]').classes()).toContain('grow')
    expect(
      wrapper.findAll('[data-usage-month]').map((label) => label.text())
    ).toEqual(expect.arrayContaining(['1月', '2月', '3月']))
    const bottom = wrapper.get('[data-usage-analytics="bottom"]')
    expect(bottom.text()).toContain('本段最活跃')
    expect(bottom.text()).toContain('星期节律')
    expect(bottom.findAll('[data-usage-segment-compact="true"]')).toHaveLength(
      2
    )
    // From sm the band drops the ranked rows and names the busiest day instead.
    expect(bottom.get('[data-usage-segment-details]').classes()).toContain(
      'sm:hidden'
    )
    expect(
      bottom
        .get('[data-usage-segment-bar="ranked"] [data-usage-segment-summary]')
        .text()
    ).toContain('365')

    const segment = wrapper.get(
      '[data-usage-analytics="bottom"] [data-usage-segment]'
    )
    await segment.trigger('mouseenter')
    expect(wrapper.get('[role="tooltip"]').text()).not.toBe('')
  })

  it('builds proportional segment bars with one focus stop each', async () => {
    const wrapper = render()
    const ranked = wrapper.get('[data-usage-segment-bar="ranked"]')
    const rhythm = wrapper.get('[data-usage-segment-bar="rhythm"]')
    const rankedSegments = ranked.findAll('[data-usage-segment]')

    expect(rankedSegments).toHaveLength(3)
    expect(rhythm.findAll('[data-usage-segment]')).toHaveLength(7)
    expect(
      rankedSegments.reduce(
        (sum, segment) =>
          sum + Number(segment.attributes('data-usage-segment-share')),
        0
      )
    ).toBeCloseTo(1, 5)
    expect(ranked.findAll('[data-usage-segment][tabindex="0"]')).toHaveLength(1)
    expect(rhythm.findAll('[data-usage-segment][tabindex="0"]')).toHaveLength(1)

    const first = ranked.get('[data-usage-segment][tabindex="0"]')
    const start = first.attributes('data-usage-segment')
    await first.trigger('focus')
    await first.trigger('keydown', { key: 'ArrowRight' })
    await Promise.resolve()
    expect(
      (document.activeElement as HTMLElement).dataset.usageSegment
    ).not.toBe(start)
  })

  it('keeps a rhythm slot under each weekday label, idle days included', () => {
    const weekdaysOnly = points().map((point) => {
      const day = new Date(`${point.date}T00:00:00`).getDay()
      return day === 0 || day === 6 ? { ...point, requests: 0 } : point
    })
    const rhythm = render({ points: weekdaysOnly }).get(
      '[data-usage-segment-bar="rhythm"]'
    )
    const segments = rhythm.findAll('[data-usage-segment]')

    expect(
      segments.map((segment) => segment.attributes('data-usage-segment'))
    ).toEqual(['0', '1', '2', '3', '4', '5', '6'])
    expect(segments[5]!.attributes('data-usage-segment-share')).toBe('0.000000')
    expect(rhythm.findAll('[data-usage-segment-labels] > span')).toHaveLength(7)
  })

  it('moves month focus by day with horizontal arrow keys', async () => {
    const wrapper = render()
    const focused = wrapper.get('[data-usage-date][tabindex="0"]')
    const start = focused.attributes('data-usage-date')
    await focused.trigger('focus')
    await focused.trigger('keydown', { key: 'ArrowLeft' })
    await Promise.resolve()

    const active = document.activeElement as HTMLElement
    const expected = new Date(`${start}T00:00:00`)
    expected.setDate(expected.getDate() - 1)
    expect(active.dataset.usageDate).toBe(dateKey(expected))
  })

  it('renders complete heatmap layout by default when empty', () => {
    const loading = render({ loading: true })
    expect(loading.find('[data-usage-loading]').exists()).toBe(true)
    loading.unmount()

    const empty = render({ points: [] })
    expect(empty.find('[data-usage-content]').exists()).toBe(true)
    expect(empty.find('[data-usage-layout="month"]').exists()).toBe(true)
    expect(empty.findAll('[data-usage-date]')).toHaveLength(30)
    expect(empty.get('[data-usage-footer]').text()).toContain('活跃 0 天')
  })

  it('keeps analytics slots in place without data', () => {
    const wrapper = render({ points: [] })
    const ranked = wrapper.get('[data-usage-segment-bar="ranked"]')
    const rhythm = wrapper.get('[data-usage-segment-bar="rhythm"]')

    expect(ranked.findAll('[data-usage-segment-placeholder]')).toHaveLength(3)
    expect(rhythm.findAll('[data-usage-segment-labels] > span').length).toBe(7)
    expect(
      wrapper
        .findAll('[data-usage-segment-summary]')
        .map((summary) => summary.text())
    ).toEqual(['暂无数据', '暂无数据'])
  })
})
