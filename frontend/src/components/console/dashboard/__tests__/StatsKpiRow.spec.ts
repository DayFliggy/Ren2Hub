import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import StatsKpiRow from '@/components/console/dashboard/stats/StatsKpiRow.vue'
import type { StatsKpi } from '@/composables/useDashboardStats'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

function render(successRate: number | null) {
  const kpi: StatsKpi = {
    totalTokens: 3_000,
    totalQuota: '1500000',
    totalRequests: 30,
    avgLatency: 1200,
    successRate,
  }
  return mount(StatsKpiRow, {
    props: { kpi },
    global: { plugins: [i18n] },
  })
}

describe('StatsKpiRow', () => {
  it('draws an unknown success ring instead of 0% when no rate was sampled', () => {
    const ring = render(null).getComponent(MiniRing)
    expect(ring.props('indeterminate')).toBe(true)
    expect(ring.find('circle[transform]').exists()).toBe(false)
  })

  it('draws the measured success rate as an arc', () => {
    const wrapper = render(99.5)
    const ring = wrapper.getComponent(MiniRing)
    expect(ring.props('indeterminate')).toBe(false)
    expect(ring.props('percent')).toBe(99.5)
    expect(wrapper.text()).toContain('99.5')
  })
})
