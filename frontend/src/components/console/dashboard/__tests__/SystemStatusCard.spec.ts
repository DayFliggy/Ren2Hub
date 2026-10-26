import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import SystemStatusCard from '@/components/console/dashboard/SystemStatusCard.vue'
import type { SystemStatus } from '@/api/systemStatus'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { resourceFixture, systemStatusFixture } from './systemStatusFixture'

beforeAll(async () => {
  await loadMessageDomain('console')
})
beforeEach(async () => {
  setActivePinia(createPinia())
  await setLocale('en')
})
function render(
  metrics: SystemStatus | null = systemStatusFixture(),
  error?: string,
  loading = false
) {
  return mount(SystemStatusCard, {
    props: { metrics, error, loading },
    global: { plugins: [i18n] },
  })
}

describe('SystemStatusCard', () => {
  it.each([
    ['ok', 'HEALTHY'],
    ['warning', 'WARNING'],
    ['critical', 'CRITICAL'],
    ['stale', 'STALE'],
    ['unknown', 'UNKNOWN'],
  ] as const)('reports the server health %s on the pill', (status, label) => {
    const metrics = systemStatusFixture()
    metrics.status = status
    const pill = render(metrics).get('[data-system-health]')
    expect(pill.attributes('data-system-health')).toBe(status)
    expect(pill.text().toUpperCase()).toContain(label)
  })
  it('distinguishes loading, failure and no snapshot', () => {
    expect(
      render(null, undefined, true).get('[data-system-health]').text()
    ).toMatch(/loading/i)
    const failed = render(null, 'snapshot unavailable').get(
      '[data-system-health]'
    )
    expect(failed.attributes('data-system-health')).toBe('error')
    expect(failed.attributes('title')).toBe('snapshot unavailable')
    expect(failed.text()).toContain('snapshot unavailable')
    expect(
      render(null).get('[data-system-health]').attributes('data-system-health')
    ).toBe('none')
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
      /5\.2\s*\/\s*16\s*GiB/
    )
    expect(wrapper.get('[data-metric="disk"]').text()).toMatch(
      /218\s*GiB\s*\/\s*512\s*GiB/
    )
    const traffic = wrapper.get('[data-metric="bandwidth"]')
    expect(traffic.text()).toContain('↑2.1 Mbps')
    expect(traffic.text()).toContain('↓12.4 Mbps')
    expect(traffic.find('[data-bandwidth-sparkline]').exists()).toBe(true)
  })
  it('shows the measured success rate and host load per core', () => {
    const wrapper = render()
    expect(wrapper.find('[data-success-rate-empty]').exists()).toBe(false)
    expect(wrapper.text()).toContain('99.4%')
    const cells = wrapper.findAll('[data-metric="cpu"] [data-minutes]')
    expect(cells.map((cell) => cell.attributes('data-unknown'))).toEqual([
      undefined,
      undefined,
      undefined,
    ])
    expect(wrapper.get('[data-load-readout]').text()).toContain('4 cores')
    expect(wrapper.get('[data-load-readout]').text()).toContain('1.20')
  })
  it('keeps a measured zero success rate distinct from an empty window', () => {
    const metrics = systemStatusFixture()
    metrics.success_rate.rate = 0
    expect(render(metrics).text()).toContain('0%')
    expect(render(metrics).find('[data-success-rate-empty]').exists()).toBe(
      false
    )
  })
  it.each([
    ['no_requests', 'No requests'],
    ['disabled', 'Disabled'],
    ['error', 'Unavailable'],
  ] as const)('labels a %s success rate without a figure', (state, label) => {
    const metrics = systemStatusFixture()
    metrics.success_rate = { ...metrics.success_rate, state, rate: null }
    const empty = render(metrics).get('[data-success-rate-empty]')
    expect(empty.text()).toBe(label)
    expect(empty.attributes('data-success-rate-state')).toBe(state)
  })
  it('leaves load cells unmeasured without a load sample', () => {
    const metrics = systemStatusFixture()
    metrics.load = null
    const cells = render(metrics).findAll('[data-metric="cpu"] [data-minutes]')
    expect(cells).toHaveLength(3)
    for (const cell of cells)
      expect(cell.attributes('data-unknown')).toBeDefined()
  })
  it("names each reading's measurement scope", () => {
    const scopes = render()
      .findAll('[data-metric-scope]')
      .map((node) => node.text())
    expect(scopes).toEqual([
      'Container',
      'Container',
      'App HTTP bodies',
      'Cache directory filesystem',
    ])
  })
  it.each(['unknown', 'disabled', 'stale'] as const)(
    'leaves a %s CPU reading unmeasured',
    (state) => {
      const metrics = systemStatusFixture()
      metrics.cpu = resourceFixture({
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
    const metrics = systemStatusFixture()
    metrics.stale = true
    const wrapper = render(metrics)
    expect(wrapper.get('[data-metric="memory"]').text()).not.toContain('5.2')
    expect(wrapper.get('[data-metric="bandwidth"]').text()).not.toContain(
      'Mbps'
    )
    expect(wrapper.find('[data-bandwidth-sparkline]').exists()).toBe(false)
  })
  it('shows the measured CPU figure but caps the gauge', () => {
    const metrics = systemStatusFixture()
    metrics.cpu.percent = 150
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
  it('converts large byte counts to TiB', () => {
    const metrics = systemStatusFixture()
    metrics.disk.used_bytes = 1536 * 1024 ** 3
    metrics.disk.total_bytes = 2048 * 1024 ** 3
    expect(render(metrics).get('[data-metric="disk"]').text()).toMatch(
      /1\.5\s*TiB\s*\/\s*2\s*TiB/
    )
  })
  it('breaks the traffic line at an unmeasured bucket', () => {
    const metrics = systemStatusFixture()
    metrics.traffic.points = [0, 1, 2, 3, 4].map((i) => ({
      ts: 1_798_539_760 + i * 60,
      request_mbps: i === 2 ? null : 1 + i,
      response_mbps: i === 2 ? null : 8 + i,
    }))
    const tile = render(metrics).get('[data-metric="bandwidth"]')
    const line = tile.findAll('[data-bandwidth-sparkline] path[fill="none"]')
    expect(line).toHaveLength(2)
    // Each direction draws two runs, one per side of the gap.
    for (const path of line)
      expect(path.attributes('d')?.match(/M/g)).toHaveLength(2)
  })
  it('does not invent a missing traffic direction', () => {
    const metrics = systemStatusFixture()
    metrics.traffic.request_mbps = null
    const tile = render(metrics).get('[data-metric="bandwidth"]')
    expect(tile.text()).toContain('--')
    expect(tile.text()).not.toContain('Mbps')
  })
  it('renders Chinese labels without raw keys', async () => {
    await setLocale('zh-CN')
    const wrapper = render()
    expect(wrapper.text()).toContain('API 成功率')
    expect(wrapper.text()).not.toContain('dashboard.')
  })
})
