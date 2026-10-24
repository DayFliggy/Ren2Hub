import { createPinia, setActivePinia } from 'pinia'
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

import MiniSparkline from '@/components/console/dashboard/MiniSparkline.vue'
import OverviewKpiStrip from '@/components/console/dashboard/OverviewKpiStrip.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type {
  DashboardStats,
  FlowPoint,
  TokenTrendPoint,
} from '@/composables/useDashboard'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
})

const stats: DashboardStats = {
  quota: '5200000',
  used_quota: '2985000',
  today_quota: '96402',
  month_quota: '1842600',
  today_requests: 312,
  today_tokens: 4_000_000,
  total_requests: 18_764,
}

const flowPoints: FlowPoint[] = Array.from({ length: 14 }, (_, i) => ({
  date: `07-${String(i + 13).padStart(2, '0')}`,
  consume: String(60_000 + i * 4_000),
  requests: 200 + i * 15,
}))

function trendPoint(over: Partial<TokenTrendPoint> = {}): TokenTrendPoint {
  return {
    date: '2026-07-26',
    total: 4_000_000,
    cache_create: 250_000,
    cache_read: 2_250_000,
    ...over,
  }
}

function render(props: Record<string, unknown> = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(OverviewKpiStrip, {
    props: { stats, ...props },
    global: { plugins: [pinia, i18n] },
  })
}

describe('OverviewKpiStrip', () => {
  it('shows five cells and no month-over-month figures', () => {
    const wrapper = render()

    expect(wrapper.findAll('[class*="px-5"]')).toHaveLength(5)
    // The month deltas were removed from this strip entirely.
    expect(wrapper.text()).not.toContain('16.2')
    expect(wrapper.text()).not.toContain('5.8')
  })

  it('keeps the account request count visible while export-backed usage is unavailable', () => {
    const wrapper = render({
      stats: {
        quota: '5200000',
        used_quota: '2985000',
        total_requests: 18_764,
      },
    })
    const requests = wrapper.get('[data-kpi-cell="requests"]')
    expect(requests.text()).toContain('Total requests')
    expect(requests.text()).toContain('18,764')
    expect(requests.text()).not.toContain('Today requests')
  })

  it('draws sparklines only for measured usage series', () => {
    const wrapper = render({
      flow: flowPoints,
      tokenTrend: [trendPoint({ date: '2026-07-25' }), trendPoint()],
      limits: {
        enabled: true,
        window_minutes: 5,
        total_requests: 300,
        successful_requests: 250,
      },
    })

    // Limits describe configuration, so they have no fabricated trend.
    expect(wrapper.findAllComponents(MiniSparkline)).toHaveLength(4)
  })

  it('omits a sparkline when a series has too few points to draw', () => {
    const wrapper = render({
      flow: flowPoints.slice(0, 1),
      tokenTrend: [trendPoint()],
    })

    expect(wrapper.findAllComponents(MiniSparkline)).toHaveLength(0)
  })

  it('keeps the real limit window and its two ceilings', () => {
    const wrapper = render({
      limits: {
        enabled: true,
        window_minutes: 5,
        total_requests: 300,
        successful_requests: 250,
      },
    })
    const cell = wrapper.get('[data-kpi-cell="requestLimits"]')
    expect(cell.text()).toContain('5-minute window')
    expect(cell.text()).toContain('Total: 300')
    expect(cell.text()).toContain('Successful: 250')
    expect(cell.text()).not.toContain('RPM')
  })

  it('treats nonpositive ceilings as unlimited independently', () => {
    const cell = render({
      limits: {
        enabled: true,
        window_minutes: 2,
        total_requests: 0,
        successful_requests: -1,
      },
    }).get('[data-kpi-cell="requestLimits"]')
    expect(cell.text()).toContain('Total: Unlimited')
    expect(cell.text()).toContain('Successful: Unlimited')
  })

  it('does not describe disabled limits as an active window', () => {
    const cell = render({
      limits: {
        enabled: false,
        window_minutes: 5,
        total_requests: 300,
        successful_requests: 250,
      },
    }).get('[data-kpi-cell="requestLimits"]')
    expect(cell.text()).toContain('Not enabled')
    expect(cell.text()).not.toContain('5-minute window')
    expect(cell.text()).not.toContain('Total:')
  })

  it.each([0, -1])(
    'reports an invalid enabled window of %s',
    (window_minutes) => {
      const cell = render({
        limits: {
          enabled: true,
          window_minutes,
          total_requests: 300,
          successful_requests: 250,
        },
      }).get('[data-kpi-cell="requestLimits"]')
      expect(cell.text()).toContain('Invalid time window')
      expect(cell.text()).not.toContain('Total:')
    }
  )

  it('loads the request limits independently of usage', () => {
    const wrapper = render({ limitsLoading: true })
    expect(
      wrapper
        .get('[data-kpi-cell="requestLimits"]')
        .find('.animate-pulse')
        .exists()
    ).toBe(true)
    expect(wrapper.get('[data-kpi-cell="requests"]').text()).toContain('312')
  })

  it('retries only limits after a supplemental failure', async () => {
    const wrapper = render({ limitsError: 'settings unavailable' })
    const cell = wrapper.get('[data-kpi-cell="requestLimits"]')
    expect(cell.text()).toContain('Failed to load')
    await cell.get('button').trigger('click')
    expect(wrapper.emitted('retryLimits')).toEqual([[]])
    expect(wrapper.emitted('switchTab')).toBeUndefined()
    expect(wrapper.get('[data-kpi-cell="requests"]').text()).toContain('312')
  })

  it("reads today's tokens from the today window when the trend is skipped", () => {
    const wrapper = render({ tokenTrend: [] })

    expect(
      wrapper.find('[data-kpi-cell="tokens"] .display-number').text()
    ).toBe('4.0M')
  })

  it('keeps the today headline independent from the trend window', () => {
    const wrapper = render({ tokenTrend: [trendPoint({ total: 123 })] })

    expect(
      wrapper.find('[data-kpi-cell="tokens"] .display-number').text()
    ).toBe('4.0M')
  })

  it('marks the rate-limit cell as not provided when limits are null', () => {
    const wrapper = render({ limits: null, loading: true })
    const cell = wrapper.find('[data-kpi-cell="requestLimits"]')

    expect(cell.text()).toContain('Not provided')
    // A missing response is distinct from unlimited configuration.
    expect(cell.text()).not.toContain('Unmetered')
  })

  it('reads 未提供 for null limits in Chinese', async () => {
    await setLocale('zh-CN')
    const text = render({ limits: null })
      .find('[data-kpi-cell="requestLimits"]')
      .text()
    await setLocale('en')

    expect(text).toContain('未提供')
    expect(text).not.toContain('不限速')
  })

  it('floors the TPM divisor at one hour so just-after-midnight is not absurd', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 6, 26, 0, 1))

    const wrapper = render()

    // 4M tokens / 60 min = 66.7K, not 4M / 1 min.
    expect(wrapper.text()).toContain('66.7K')
  })

  it('averages TPM over the elapsed day', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 6, 26, 10, 0))

    const wrapper = render()

    // 4M / 600 min = 6.7K
    expect(wrapper.text()).toContain('6.7K')
  })

  it('only makes the spend and request cells drill down', () => {
    const wrapper = render({
      limits: {
        enabled: true,
        window_minutes: 5,
        total_requests: 300,
        successful_requests: 250,
      },
    })

    const drillDowns = wrapper
      .findAll('[class*="px-5"]')
      .filter((cell) => cell.element.tagName === 'BUTTON')

    expect(drillDowns).toHaveLength(2)
    expect(drillDowns[0]!.text()).toContain('Today spend')
    expect(drillDowns[1]!.text()).toContain('Today requests')
  })

  it('emits switchTab when a drill-down cell is clicked', async () => {
    const wrapper = render()
    await wrapper.findAll('[class*="px-5"]')[0]!.trigger('click')

    expect(wrapper.emitted('switchTab')).toEqual([['stats']])
  })

  it('always pairs a divider width with the theme border colour', () => {
    const wrapper = render()

    wrapper.findAll('[class*="px-5"]').forEach((cell) => {
      const classes = cell.classes()
      expect(classes).toContain('border-[var(--border-subtle)]')
      // Every axis is stated at every breakpoint, so no toggle can fall through
      // to Tailwind's default grey.
      ;[
        'border-l',
        'border-t',
        'sm:border-l',
        'sm:border-t',
        'xl:border-l',
      ].forEach((axis) => {
        expect(
          classes.some((c) => c === axis || c === `${axis}-0`),
          `expected ${axis} or ${axis}-0`
        ).toBe(true)
      })
    })
  })

  it('hides spend but not request counts when balances are masked', () => {
    localStorage.setItem('renren_hide_balance', 'true')
    const wrapper = render()

    expect(wrapper.text()).toContain('••••')
    expect(wrapper.text()).toContain('312')
  })
})
