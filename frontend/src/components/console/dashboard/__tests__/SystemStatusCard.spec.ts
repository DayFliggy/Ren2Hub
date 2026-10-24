import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import SystemStatusCard from '@/components/console/dashboard/SystemStatusCard.vue'
import type { Observability } from '@/api/observability'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores'
import { formatTime } from '@/utils/format'
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
  props: { loading?: boolean; error?: string } = {}
) {
  return mount(SystemStatusCard, {
    props: { metrics, ...props },
    global: { plugins: [i18n] },
  })
}

describe('SystemStatusCard native observation snapshot', () => {
  it('reports public API reachability separately from resource availability', () => {
    const app = useAppStore()
    app.phase = 'ready'
    app.statusReachable = true
    const wrapper = render(null)
    expect(wrapper.text()).toContain('ONLINE')
    expect(wrapper.text()).toContain('Not provided')
    expect(
      wrapper.get('[data-status-reachable]').attributes('data-status-reachable')
    ).toBe('true')
    expect(wrapper.text()).not.toContain('0%')
  })
  it('does not claim healthy API state before status is known', () => {
    const wrapper = render(null)
    expect(wrapper.text()).toContain('UNKNOWN')
    expect(wrapper.text()).not.toContain('ONLINE')
  })
  it('keeps distinct resource gauges and native binary capacities', () => {
    const wrapper = render()
    expect(wrapper.findAll('[data-system-status-tile]')).toHaveLength(4)
    expect(wrapper.get('[data-metric="cpu"] [data-metric-value]').text()).toBe(
      '34'
    )
    expect(wrapper.find('[data-cpu-gauge-active]').exists()).toBe(true)
    expect(wrapper.findAll('[data-memory-segments] > span')).toHaveLength(10)
    expect(wrapper.get('[data-metric="memory"]').text()).toMatch(
      /5\.2\s*\/\s*16\s*GiB/
    )
    expect(wrapper.get('[data-metric="disk"]').text()).toMatch(
      /218\s*GiB\s*\/\s*512\s*GiB/
    )
    expect(wrapper.find('[data-disk-gauge]').exists()).toBe(true)
    expect(wrapper.find('[data-success-rate-ring]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('cores')
    expect(wrapper.text()).not.toContain('Load average')
  })
  it('shows each resource scope and timestamp from the same snapshot', () => {
    const metrics = observabilityFixture()
    metrics.resources.cpu.scope = 'host'
    const wrapper = render(metrics)
    expect(
      wrapper.get('[data-metric="cpu"] [data-resource-caption]').text()
    ).toContain('Host · Measured')
    expect(
      wrapper.get('[data-metric="memory"] [data-resource-caption]').text()
    ).toContain('Container · Measured')
    expect(
      wrapper.get('[data-metric="disk"] [data-resource-caption]').text()
    ).toContain('Filesystem · Measured')
    expect(wrapper.get('[data-resource-sampled-at]').text()).toContain(
      formatTime(metrics.sampled_at!)
    )
  })
  it.each(['unknown', 'disabled', 'stale'] as const)(
    'leaves %s resource values unmeasured',
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
      expect(wrapper.find('[data-cpu-gauge-unknown]').exists()).toBe(true)
    }
  )
  it('keeps an over-capacity CPU headline without overflowing the decorative gauge', () => {
    const metrics = observabilityFixture()
    metrics.resources.cpu.percent = 150
    const wrapper = render(metrics)
    expect(wrapper.get('[data-metric="cpu"] [data-metric-value]').text()).toBe(
      '150'
    )
    expect(
      wrapper.get('[data-cpu-gauge-active]').attributes('stroke-dashoffset')
    ).toBe('0')
    expect(
      wrapper.get('[data-metric="cpu"]').attributes('data-load-tier')
    ).toBe('danger')
  })
  it('formats large storage in TiB while preserving the capacity', () => {
    const metrics = observabilityFixture()
    metrics.resources.disk.used_bytes = 1536 * 1024 ** 3
    metrics.resources.disk.total_bytes = 2048 * 1024 ** 3
    expect(render(metrics).get('[data-metric="disk"]').text()).toMatch(
      /1\.5\s*TiB\s*\/\s*2\s*TiB/
    )
  })
  it('labels traffic as application body throughput and uses native history', () => {
    const tile = render().get('[data-metric="bandwidth"]')
    expect(tile.text()).toContain('Request 2.1 Mbps')
    expect(tile.text()).toContain('Response 12.4 Mbps')
    expect(tile.text()).toContain('Application HTTP body throughput')
    expect(tile.find('[data-bandwidth-sparkline]').exists()).toBe(true)
  })
  it('does not invent a missing traffic direction or bridge a history gap', () => {
    const metrics = observabilityFixture()
    metrics.traffic.request_mbps = null
    metrics.traffic.response_mbps = 0.002
    metrics.series.points[1]!.request_mbps = null
    const tile = render(metrics).get('[data-metric="bandwidth"]')
    expect(tile.text()).toContain('Request --')
    expect(tile.text()).toContain('Response 2 Kbps')
    expect(tile.find('[data-bandwidth-sparkline]').exists()).toBe(false)
  })
  it('does not draw history from a single sample', () => {
    const metrics = observabilityFixture()
    metrics.series.points = metrics.series.points.slice(0, 1)
    expect(render(metrics).find('[data-bandwidth-sparkline]').exists()).toBe(
      false
    )
  })
  it('marks stale snapshots and withholds their measured values', () => {
    const metrics = observabilityFixture()
    metrics.stale = true
    const wrapper = render(metrics)
    expect(wrapper.get('[role="status"]').text()).toBe('Stale data')
    expect(wrapper.get('[data-metric="cpu"] [data-metric-value]').text()).toBe(
      '--'
    )
    expect(wrapper.get('[data-metric="memory"]').text()).not.toContain('5.2')
    expect(wrapper.get('[data-metric="bandwidth"]').text()).not.toContain(
      'Mbps'
    )
    expect(wrapper.find('[data-bandwidth-sparkline]').exists()).toBe(false)
  })
  it('shows a read error and emits a focused retry', async () => {
    const wrapper = render(null, { error: 'snapshot unavailable' })
    expect(wrapper.get('[role="alert"]').text()).toBe('snapshot unavailable')
    await wrapper.get('header button').trigger('click')
    expect(wrapper.emitted('refresh')).toEqual([[]])
  })
  it('disables duplicate refreshes while a snapshot is loading', () => {
    expect(
      render(null, { loading: true })
        .get('header button')
        .attributes('disabled')
    ).toBeDefined()
  })
  it('shows resource scope and sampling labels in Chinese', async () => {
    await setLocale('zh-CN')
    const wrapper = render()
    expect(wrapper.get('[data-metric="cpu"]').text()).toContain('容器 · 已采集')
    expect(wrapper.get('[data-resource-sampled-at]').text()).toContain(
      '快照采样'
    )
    expect(wrapper.text()).not.toContain('dashboard.')
  })
})
