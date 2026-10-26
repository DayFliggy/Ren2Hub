import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import SystemStatusCard from '@/components/console/dashboard/SystemStatusCard.vue'
import type { Observability } from '@/api/observability'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores'
import { observabilityFixture, resourceFixture } from './observabilityFixture'

beforeAll(async () => {
  await loadMessageDomain('console')
})
beforeEach(async () => {
  setActivePinia(createPinia())
  await setLocale('en')
})
function render(
  metrics: Observability | null = observabilityFixture(),
  error?: string
) {
  return mount(SystemStatusCard, {
    props: { metrics, error },
    global: { plugins: [i18n] },
  })
}

describe('SystemStatusCard', () => {
  it('reports online only when the API is reachable and a snapshot exists', () => {
    const app = useAppStore()
    app.phase = 'ready'
    app.statusReachable = true
    expect(render().text()).toContain('ONLINE')
    const withoutSnapshot = render(null)
    expect(withoutSnapshot.text()).toContain('UNKNOWN')
    expect(withoutSnapshot.text()).not.toContain('ONLINE')
  })
  it('keeps the last snapshot error on the status pill', () => {
    const pill = render(null, 'snapshot unavailable').get(
      '[data-status-reachable]'
    )
    expect(pill.attributes('title')).toBe('snapshot unavailable')
  })
  it('renders placeholders rather than zeroes without a snapshot', () => {
    const wrapper = render(null)
    expect(wrapper.findAll('[data-system-status-tile]')).toHaveLength(4)
    expect(wrapper.get('[data-metric="cpu"] [data-metric-value]').text()).toBe(
      '--'
    )
    expect(wrapper.find('[data-cpu-gauge-unknown]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('0%')
  })
  it('maps snapshot resources onto the CPU, memory, traffic and disk gauges', () => {
    const wrapper = render()
    expect(wrapper.get('[data-metric="cpu"] [data-metric-value]').text()).toBe(
      '34'
    )
    expect(wrapper.find('[data-cpu-gauge-active]').exists()).toBe(true)
    expect(wrapper.get('[data-metric="memory"]').text()).toMatch(
      /5\.2\s*\/\s*16\s*GB/
    )
    expect(wrapper.get('[data-metric="disk"]').text()).toMatch(
      /218\s*GB\s*\/\s*512\s*GB/
    )
    const traffic = wrapper.get('[data-metric="bandwidth"]')
    expect(traffic.text()).toContain('↑2.1 Mbps')
    expect(traffic.text()).toContain('↓12.4 Mbps')
    expect(traffic.find('[data-bandwidth-sparkline]').exists()).toBe(true)
  })
  it('shows the success rate and load cells as unavailable, never as zero', () => {
    const wrapper = render()
    expect(wrapper.get('[data-success-rate-empty]').text()).toBe('No data')
    expect(
      wrapper
        .get('[data-success-rate-ring]')
        .attributes('data-success-rate-state')
    ).toBe('unknown')
    const cells = wrapper.findAll('[data-metric="cpu"] [data-minutes]')
    expect(cells).toHaveLength(3)
    for (const cell of cells)
      expect(cell.attributes('data-unknown')).toBeDefined()
  })
  it.each(['unknown', 'disabled', 'stale'] as const)(
    'leaves a %s CPU reading unmeasured',
    (state) => {
      const metrics = observabilityFixture()
      metrics.resources.cpu = resourceFixture({
        available: state === 'stale',
        percent: state === 'stale' ? 34 : null,
        state,
      })
      const wrapper = render(metrics)
      expect(
        wrapper.get('[data-metric="cpu"] [data-metric-value]').text()
      ).toBe('--')
      expect(wrapper.find('[data-cpu-gauge-active]').exists()).toBe(false)
    }
  )
  it('withholds every measured value from a stale snapshot', () => {
    const metrics = observabilityFixture()
    metrics.stale = true
    const wrapper = render(metrics)
    expect(wrapper.get('[data-metric="memory"]').text()).not.toContain('5.2')
    expect(wrapper.get('[data-metric="bandwidth"]').text()).not.toContain(
      'Mbps'
    )
    expect(wrapper.find('[data-bandwidth-sparkline]').exists()).toBe(false)
  })
  it('caps an over-capacity CPU gauge and marks it danger', () => {
    const metrics = observabilityFixture()
    metrics.resources.cpu.percent = 150
    const wrapper = render(metrics)
    expect(
      wrapper.get('[data-cpu-gauge-active]').attributes('stroke-dashoffset')
    ).toBe('0')
    expect(
      wrapper.get('[data-metric="cpu"]').attributes('data-load-tier')
    ).toBe('danger')
  })
  it('converts large byte counts to TB', () => {
    const metrics = observabilityFixture()
    metrics.resources.disk.used_bytes = 1536 * 1024 ** 3
    metrics.resources.disk.total_bytes = 2048 * 1024 ** 3
    expect(render(metrics).get('[data-metric="disk"]').text()).toMatch(
      /1\.5\s*TB\s*\/\s*2\s*TB/
    )
  })
  it('does not bridge a traffic history gap or invent a direction', () => {
    const metrics = observabilityFixture()
    metrics.traffic.request_mbps = null
    metrics.series.points[1]!.request_mbps = null
    const tile = render(metrics).get('[data-metric="bandwidth"]')
    expect(tile.text()).toContain('--')
    expect(tile.text()).not.toContain('Mbps')
    expect(tile.find('[data-bandwidth-sparkline]').exists()).toBe(false)
  })
  it('renders Chinese labels without raw keys', async () => {
    await setLocale('zh-CN')
    const wrapper = render()
    expect(wrapper.text()).toContain('API 成功率')
    expect(wrapper.text()).not.toContain('dashboard.')
  })
})
