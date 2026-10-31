import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import SystemStatusCard from '@/components/console/dashboard/SystemStatusCard.vue'
import type { SystemStatus } from '@/api/systemStatus'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { resourceFixture, systemStatusFixture } from './systemStatusFixture'

const gib = 1024 ** 3

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
    ).toBe('Loading')
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
    const wrapper = render(null, undefined, true)
    expect(wrapper.findAll('[data-system-status-tile]')).toHaveLength(4)
    expect(
      wrapper.findAll('[data-metric-placeholder]').map((node) => node.text())
    ).toEqual(['--', '--', '--', '--'])
    expect(wrapper.find('[data-metric-value]').exists()).toBe(false)
    expect(wrapper.find('[data-cpu-gauge-unknown]').exists()).toBe(true)
    expect(wrapper.get('[data-success-rate-empty]').text()).toBe('--')
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
    expect(traffic.text()).toContain('↑12.4 Mbps')
    expect(traffic.text()).toContain('↓2.1 Mbps')
    expect(traffic.find('[data-bandwidth-sparkline]').exists()).toBe(true)
  })
  it('shows traffic from the server side: up is outbound, down inbound', () => {
    const metrics = systemStatusFixture()
    metrics.traffic.inbound_mbps = 1
    metrics.traffic.outbound_mbps = 7
    metrics.traffic.points = [0, 1, 2].map((i) => ({
      ts: 1_798_539_880 + i * 60,
      inbound_mbps: 1,
      outbound_mbps: 7,
    }))
    const tile = render(metrics).get('[data-metric="bandwidth"]')
    const up = tile.get('[data-bandwidth-direction="up"]')
    const down = tile.get('[data-bandwidth-direction="down"]')
    expect(up.text()).toBe('↑7 Mbps')
    expect(down.text()).toBe('↓1 Mbps')
    // Each figure pairs with the line in its ink; the larger outbound rate
    // sits at the top of the shared scale (y 2), inbound at the bottom (y 14).
    const ink = (node: typeof up) =>
      node
        .classes()
        .join(' ')
        .match(/text-\[(var\(--[a-z-]+\))\]/)![1]
    const lineTop = (color: string) =>
      tile
        .get(`[data-bandwidth-sparkline] path[fill="none"][stroke="${color}"]`)
        .attributes('d')!
        .match(/^M[\d.]+ ([\d.]+)/)![1]
    expect(lineTop(ink(up)!)).toBe('2.00')
    expect(lineTop(ink(down)!)).toBe('14.00')
    const words = 'Server interface, outbound 7 Mbps, inbound 1 Mbps'
    expect(tile.get('[data-metric-scope]').text()).toBe(words)
    expect(tile.find(`[title="${words}"]`).text()).toBe('Traffic')
  })
  it.each([
    [
      'host_interface',
      'en',
      'Traffic',
      'Server interface, outbound 12.4 Mbps, inbound 2.1 Mbps',
    ],
    [
      'application_listener',
      'en',
      'Traffic',
      'App connections, outbound 12.4 Mbps, inbound 2.1 Mbps',
    ],
    [
      'host_interface',
      'zh-CN',
      '流量',
      '服务器网卡，出站 12.4 Mbps，入站 2.1 Mbps',
    ],
    [
      'application_listener',
      'zh-CN',
      '流量',
      '应用连接，出站 12.4 Mbps，入站 2.1 Mbps',
    ],
  ] as const)(
    'titles %s traffic in %s and names its scope',
    async (scope, locale, title, words) => {
      await setLocale(locale)
      const metrics = systemStatusFixture()
      metrics.traffic.scope = scope
      const tile = render(metrics).get('[data-metric="bandwidth"]')
      expect(tile.get('[data-metric-scope]').text()).toBe(words)
      expect(tile.get(`[title="${words}"]`).text()).toBe(title)
    }
  )
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
      'Server interface, outbound 12.4 Mbps, inbound 2.1 Mbps',
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
        wrapper.get('[data-metric="cpu"] [data-metric-placeholder]').text()
      ).toBe(state === 'disabled' ? 'Off' : '--')
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
  })
  it.each([
    ['ok', 'notice'],
    ['warning', 'warning'],
    ['critical', 'danger'],
  ] as const)(
    'colours a 93%% reading by the server verdict %s, not by a fixed share',
    (state, tier) => {
      const metrics = systemStatusFixture()
      metrics.cpu = resourceFixture({ percent: 93, state })
      metrics.memory = resourceFixture({
        percent: 93,
        state,
        used_bytes: 93 * gib,
        total_bytes: 100 * gib,
      })
      metrics.disk = resourceFixture({
        percent: 93,
        state,
        scope: 'filesystem',
        used_bytes: 930 * gib,
        total_bytes: 1000 * gib,
      })
      const wrapper = render(metrics)
      for (const key of ['cpu', 'memory', 'disk'])
        expect(
          wrapper.get(`[data-metric="${key}"]`).attributes('data-load-tier')
        ).toBe(tier)
      expect(wrapper.get('[data-metric="disk"]').text()).toContain('93%')
    }
  )
  it('keeps host load below the alarm tiers and labels it host-wide', async () => {
    const metrics = systemStatusFixture()
    metrics.load = { scope: 'host', one: 6.4, five: 3.5, fifteen: 1, cores: 4 }
    const wrapper = render(metrics)
    const cells = wrapper.findAll('[data-metric="cpu"] [data-minutes]')
    expect(cells[0]!.attributes('style')).toContain('--load-notice')
    expect(cells[1]!.attributes('style')).toContain('--load-notice')
    expect(cells[2]!.attributes('style')).toContain('--load-idle')
    const readout = wrapper.get('[data-load-readout]')
    expect(readout.text()).toContain('Host load average')
    expect(readout.get('[data-load-window="1"] dd').classes()).toContain(
      'cpu-load__figure--over'
    )
    await setLocale('zh-CN')
    expect(render(metrics).get('[data-load-readout]').text()).toContain(
      '宿主机平均负载'
    )
  })
  it('names a storage failure on the pill, which no tile shows', async () => {
    const metrics = systemStatusFixture()
    metrics.status = 'critical'
    metrics.storage_status = 'critical'
    const pill = render(metrics).get('[data-system-health]')
    expect(pill.attributes('data-system-health')).toBe('critical')
    expect(pill.text()).toContain('Storage down')
    await setLocale('zh-CN')
    expect(render(metrics).get('[data-system-health]').text()).toContain(
      '存储异常'
    )
  })
  it('headlines the share when a reading has no byte counts', () => {
    const metrics = systemStatusFixture()
    metrics.memory = resourceFixture({ percent: 96, state: 'warning' })
    const tile = render(metrics).get('[data-metric="memory"]')
    expect(tile.attributes('data-load-tier')).toBe('warning')
    expect(tile.text()).toContain('96%')
    expect(tile.find('[data-metric-placeholder]').exists()).toBe(false)
    expect(tile.find('[data-percent-badge]').exists()).toBe(false)
  })
  it.each([
    [1536, 2048, /1\.5\s*TiB\s*\/\s*2\s*TiB/],
    // 0.9 / 1 TiB beside a 93% badge read as a contradiction.
    [930, 1000, /0\.91\s*TiB\s*\/\s*0\.98\s*TiB/],
    [990, 1000, /0\.97\s*TiB\s*\/\s*0\.98\s*TiB/],
  ])('shows %i of %i GiB in TiB with three figures', (used, total, text) => {
    const metrics = systemStatusFixture()
    metrics.disk.used_bytes = used * gib
    metrics.disk.total_bytes = total * gib
    expect(render(metrics).get('[data-metric="disk"]').text()).toMatch(text)
  })
  it('breaks the traffic line at an unmeasured bucket', () => {
    const metrics = systemStatusFixture()
    metrics.traffic.points = [0, 1, 2, 3, 4].map((i) => ({
      ts: 1_798_539_760 + i * 60,
      inbound_mbps: i === 2 ? null : 1 + i,
      outbound_mbps: i === 2 ? null : 8 + i,
    }))
    const tile = render(metrics).get('[data-metric="bandwidth"]')
    const line = tile.findAll('[data-bandwidth-sparkline] path[fill="none"]')
    expect(line).toHaveLength(2)
    // Each direction draws two runs, one per side of the gap.
    for (const path of line)
      expect(path.attributes('d')?.match(/M/g)).toHaveLength(2)
  })
  it.each(['inbound_mbps', 'outbound_mbps'] as const)(
    'does not invent a missing traffic direction (%s null)',
    (field) => {
      const metrics = systemStatusFixture()
      metrics.traffic[field] = null
      const tile = render(metrics).get('[data-metric="bandwidth"]')
      expect(tile.get('[data-metric-placeholder]').text()).toBe('--')
      expect(tile.text()).not.toContain('Mbps')
      expect(tile.get('[data-metric-scope]').text()).toBe('Server interface')
    }
  )
  it('shows a reading with its threshold check off without a tier', () => {
    const metrics = systemStatusFixture()
    metrics.disk = resourceFixture({
      percent: 99,
      state: 'unchecked',
      scope: 'filesystem',
      used_bytes: 507 * gib,
      total_bytes: 512 * gib,
    })
    const tile = render(metrics).get('[data-metric="disk"]')
    expect(tile.attributes('data-load-tier')).toBe('unchecked')
    expect(tile.get('[data-percent-badge]').text()).toBe('99%')
    expect(tile.get('[data-metric-scope]').text()).toBe(
      'Cache directory filesystem, threshold off'
    )
  })
  it('names an alarm tier in words, not only in colour', async () => {
    const metrics = systemStatusFixture()
    metrics.cpu = resourceFixture({ percent: 93, state: 'warning' })
    metrics.memory = resourceFixture({ percent: 97, state: 'critical' })
    const expected = [
      ['cpu', 'Container, over the warning threshold'],
      ['memory', 'Container, critical'],
      ['disk', 'Cache directory filesystem'],
    ]
    const wrapper = render(metrics)
    for (const [key, scope] of expected) {
      const tile = wrapper.get(`[data-metric="${key}"]`)
      expect(tile.get('[data-metric-scope]').text()).toBe(scope)
      // The visible label's hover text carries the same words.
      expect(tile.find(`[title="${scope}"]`).exists()).toBe(true)
    }
    await setLocale('zh-CN')
    expect(
      render(metrics).get('[data-metric="cpu"] [data-metric-scope]').text()
    ).toBe('容器，超过告警阈值')
  })
  it('does not name an alarm on a reading it withholds', () => {
    const metrics = systemStatusFixture()
    metrics.stale = true
    metrics.cpu = resourceFixture({ percent: 93, state: 'warning' })
    const tile = render(metrics).get('[data-metric="cpu"]')
    expect(tile.attributes('data-load-tier')).toBe('unknown')
    expect(tile.get('[data-metric-scope]').text()).toBe('Container')
  })
  it('dashes the memory segments only without a reading', () => {
    const segments = (percent: number | null) => {
      const metrics = systemStatusFixture()
      metrics.memory = resourceFixture(
        percent === null
          ? { available: false, percent: null, state: 'unknown' }
          : { percent, used_bytes: 0, total_bytes: 16 * gib }
      )
      return render(metrics).get('[data-memory-segments]')
    }
    const unknown = segments(null)
    expect(unknown.attributes('data-unknown')).toBe('')
    // No night glow either: the tile's own --mark-glow must not leak in.
    for (const segment of unknown.findAll('.memory-segment'))
      expect(segment.attributes('style')).toContain('--mark-glow: transparent')
    expect(segments(0).attributes('data-unknown')).toBeUndefined()
  })
  it('says resource monitoring is off instead of showing unknown', async () => {
    const metrics = systemStatusFixture()
    const off = resourceFixture({
      available: false,
      percent: null,
      state: 'disabled',
    })
    metrics.cpu = off
    metrics.memory = off
    metrics.disk = { ...off, scope: 'filesystem' }
    const wrapper = render(metrics)
    for (const key of ['cpu', 'memory', 'disk']) {
      const tile = wrapper.get(`[data-metric="${key}"]`)
      expect(tile.get('[data-metric-placeholder]').text()).toBe('Off')
      expect(tile.get('[data-metric-scope]').text()).toBe(
        'Resource monitoring is off'
      )
    }
    // Traffic is counted apart from the resource monitor and stays measured.
    expect(wrapper.get('[data-metric="bandwidth"]').text()).toContain('Mbps')
    await setLocale('zh-CN')
    expect(
      render(metrics)
        .get('[data-metric="cpu"] [data-metric-placeholder]')
        .text()
    ).toBe('未启用')
  })
  it('names the withheld readings of a stale snapshot on the pill', () => {
    const metrics = systemStatusFixture()
    metrics.status = 'stale'
    metrics.stale = true
    const pill = render(metrics).get('[data-system-health]')
    expect(pill.attributes('title')).toContain(
      'CPU, memory, disk and traffic readings are stale'
    )
    expect(pill.get('.sr-only').text()).toBe(pill.attributes('title'))
  })
  it('marks a failed success-rate query apart from an empty window', () => {
    const metrics = systemStatusFixture()
    metrics.success_rate = {
      ...metrics.success_rate,
      state: 'error',
      rate: null,
    }
    expect(
      render(metrics).get('[data-success-rate-empty]').classes()
    ).toContain('text-[var(--status-warning-text)]')
    metrics.success_rate.state = 'no_requests'
    expect(
      render(metrics).get('[data-success-rate-empty]').classes()
    ).toContain('text-[var(--text-tertiary)]')
  })
  it('draws load cells as unknown when the core count is unknown', () => {
    const metrics = systemStatusFixture()
    metrics.load = {
      scope: 'host',
      one: 1.2,
      five: 0.84,
      fifteen: 0.5,
      cores: null,
    }
    const wrapper = render(metrics)
    const cells = wrapper.findAll('[data-metric="cpu"] [data-minutes]')
    for (const cell of cells)
      expect(cell.attributes('data-unknown')).toBeDefined()
    expect(wrapper.get('[data-load-readout]').text()).toContain('1.20')
  })
  it('renders Chinese labels without raw keys', async () => {
    await setLocale('zh-CN')
    const wrapper = render()
    expect(wrapper.text()).toContain('API 成功率')
    expect(wrapper.text()).not.toContain('dashboard.')
  })
})
