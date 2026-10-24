import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'

import SystemStatusCard from '@/components/console/dashboard/SystemStatusCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { SystemMetrics } from '@/composables/useDashboard'
import { useAppStore } from '@/stores'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(async () => {
  setActivePinia(createPinia())
  await setLocale('en')
})

function metrics(): SystemMetrics {
  return {
    cpu_percent: 34,
    cpu_cores: 4,
    load_average: { one: 0.38, five: 0.2, fifteen: 0.18 },
    memory_used_gb: 5.2,
    memory_total_gb: 16,
    bandwidth_up_mbps: 2.1,
    bandwidth_down_mbps: 12.4,
    disk_used_gb: 218,
    disk_total_gb: 512,
    api_success_rate: 99.7,
    bandwidth_series: {
      up: [1.1, 1.6, 2.1],
      down: [8.6, 10.9, 12.4],
    },
  }
}

describe('SystemStatusCard', () => {
  it('starts unknown instead of claiming that the service is healthy', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.text()).toContain('UNKNOWN')
    expect(wrapper.text()).not.toContain('ONLINE')
    expect(wrapper.find('[data-status-reachable]').attributes()).toMatchObject({
      'data-status-reachable': 'false',
    })
  })

  it('reports API reachability separately from unavailable host metrics', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    app.statusReachable = true
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: null },
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.text()).toContain('ONLINE')
    expect(wrapper.text()).toContain('Not provided')
  })

  it('renders placeholders rather than zeroes when metrics are unavailable', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: null },
      global: { plugins: [pinia, i18n] },
    })

    // Four tiles plus the header readout, none of which have data yet (version
    // falls back to the store's own '--').
    expect(wrapper.text()).toContain('--')
    expect(wrapper.text()).not.toContain('0%')
  })

  it('keeps unavailable fields as placeholders when the response is partial', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          cpu_percent: null,
          cpu_cores: null,
          load_average: null,
          memory_used_gb: null,
          memory_total_gb: null,
          bandwidth_up_mbps: null,
          bandwidth_down_mbps: null,
          disk_used_gb: null,
          disk_total_gb: null,
          api_success_rate: null,
          bandwidth_series: null,
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.text()).not.toContain('null')
    expect(wrapper.text()).toContain('--')
  })

  it('reads the success rate out in the header, not as a tile', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: metrics() },
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.find('header').text()).toContain('99.7%')
    // Four resource tiles remain; success rate is no longer one of them.
    expect(wrapper.findAll('[data-system-status-tile]')).toHaveLength(4)
  })

  it('renders a prominent determinate success-rate ring for valid data', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: metrics() },
      global: { plugins: [pinia, i18n] },
    })

    const ring = wrapper.find('[data-success-rate-ring]')
    expect(ring.exists()).toBe(true)
    expect(ring.attributes('data-success-rate-state')).toBe('value')
    expect(ring.find('svg').attributes('width')).toBe('38')
    expect(ring.findAll('.tick-active').length).toBeGreaterThan(0)
    expect(ring.findAll('.tick-standby')).toHaveLength(0)
  })

  it('uses an indeterminate neutral ring when success rate is unavailable', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: { ...metrics(), api_success_rate: null } },
      global: { plugins: [pinia, i18n] },
    })

    const ring = wrapper.find('[data-success-rate-ring]')
    expect(ring.attributes('data-success-rate-state')).toBe('unknown')
    expect(ring.findAll('.tick-standby')).toHaveLength(20)
    expect(ring.findAll('.tick-active')).toHaveLength(0)
    // Without enough recent requests there is no rate: say so, not "--".
    expect(wrapper.get('[data-success-rate-empty]').text()).toBe('No data')
    expect(wrapper.find('header').text()).not.toContain('--')
  })

  it('uses a distinct visual gauge for each system resource', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: metrics() },
      global: { plugins: [pinia, i18n] },
    })

    const tiles = wrapper.findAll('[data-system-status-tile]')
    expect(tiles).toHaveLength(4)
    const [cpu, memory, bandwidth, disk] = tiles
    expect(cpu!.find('[data-cpu-gauge]').exists()).toBe(true)
    expect(cpu!.find('[data-cpu-gauge-active]').exists()).toBe(true)
    expect(memory!.find('[data-memory-segments]').exists()).toBe(true)
    expect(memory!.findAll('[data-memory-segments] > span')).toHaveLength(10)
    expect(disk!.find('[data-disk-gauge]').exists()).toBe(true)
    expect(disk!.find('svg').exists()).toBe(true)
    expect(bandwidth!.find('.pencil-progress').exists()).toBe(false)
    expect(bandwidth!.find('[data-bandwidth-sparkline]').exists()).toBe(true)
    expect(bandwidth!.find('[data-bandwidth-icon]').exists()).toBe(true)
  })

  it('shows the CPU reading once instead of repeating it in a rounded badge', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: { ...metrics(), cpu_percent: 4.627766599 } },
      global: { plugins: [pinia, i18n] },
    })

    const [cpu, memory, , disk] = wrapper.findAll('[data-system-status-tile]')
    expect(cpu!.find('[data-percent-badge]').exists()).toBe(false)
    expect(cpu!.text().match(/%/g)).toHaveLength(1)
    expect(cpu!.text()).toContain('4.6%')
    // Memory and disk headlines are in GB, so their percentage badge adds information.
    expect(memory!.find('[data-percent-badge]').exists()).toBe(true)
    expect(disk!.find('[data-percent-badge]').exists()).toBe(true)
  })

  it('colours each load cell by its share of the host cores', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          ...metrics(),
          load_average: { one: 6, five: 2.5, fifteen: 0.4 },
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    const cpu = wrapper.findAll('[data-system-status-tile]')[0]!
    const cells = cpu.findAll('[data-cpu-load] [data-minutes]')
    expect(cells).toHaveLength(3)
    // 6 / 4 queues work, 2.5 / 4 = 62.5% sits in the attention tier, 0.4 / 4 is idle.
    expect(cells[0]!.attributes('style')).toContain(
      '--tier: var(--load-danger)'
    )
    expect(cells[1]!.attributes('style')).toContain(
      '--tier: var(--load-notice)'
    )
    expect(cells[2]!.attributes('style')).toContain('--tier: var(--load-idle)')
    const figures = cpu.findAll('[data-load-readout] .cpu-load__figure')
    expect(figures.map((figure) => figure.text())).toEqual([
      '6.00',
      '2.50',
      '0.40',
    ])
    expect(figures[0]!.classes()).toContain('cpu-load__figure--over')
    expect(figures[2]!.classes()).not.toContain('cpu-load__figure--over')
  })

  it('splits resource load into five tiers at 30, 60, 75 and 90 percent', () => {
    const cases: Array<[number, string]> = [
      [0, 'idle'],
      [30, 'idle'],
      [30.1, 'normal'],
      [60, 'normal'],
      [60.1, 'notice'],
      [75, 'notice'],
      [75.1, 'warning'],
      [90, 'warning'],
      [90.1, 'danger'],
    ]
    for (const [percent, tier] of cases) {
      const pinia = createPinia()
      setActivePinia(pinia)
      const wrapper = mount(SystemStatusCard, {
        props: { metrics: { ...metrics(), cpu_percent: percent } },
        global: { plugins: [pinia, i18n] },
      })

      const [cpu, , bandwidth] = wrapper.findAll('[data-system-status-tile]')
      expect(cpu!.attributes('data-load-tier'), `${percent}%`).toBe(tier)
      expect(cpu!.find('[data-cpu-gauge-active]').attributes('stroke')).toBe(
        `var(--load-${tier})`
      )
      // Traffic has no ceiling, so it never takes a load tier.
      expect(bandwidth!.attributes('data-load-tier')).toBe('unknown')
      wrapper.unmount()
    }
  })

  it('fills a partly used memory segment by its share instead of fading it', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: { ...metrics(), memory_used_gb: 7.2, memory_total_gb: 16 },
      },
      global: { plugins: [pinia, i18n] },
    })

    // 45%: four full segments, half of the fifth, then empty track.
    const segments = wrapper.findAll('[data-memory-segments] > span')
    const styles = segments.map((segment) => segment.attributes('style') ?? '')
    expect(styles[3]).toContain('background: var(--load-normal)')
    expect(styles[4]).toContain('var(--load-normal) 50.0%')
    expect(styles[5]).not.toContain('var(--load-normal)')
    expect(styles.join(' ')).not.toContain('opacity')
  })

  it('marks load windows unknown instead of drawing zero load', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          ...metrics(),
          cpu_cores: 2.5,
          load_average: { one: 0.4, five: Number.NaN, fifteen: 0.1 },
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    const cells = wrapper.findAll('[data-cpu-load] [data-minutes]')
    // A fractional core count is invalid, so no cell claims a share of capacity.
    for (const cell of cells) {
      expect(cell.attributes('style')).toContain('--fill-height: 0%')
    }
    expect(cells[1]!.attributes('data-unknown')).toBe('')
    const figures = wrapper.findAll('[data-load-readout] .cpu-load__figure')
    expect(figures[0]!.text()).toBe('0.40')
    expect(figures[1]!.text()).toContain('--')
  })

  it('draws an unknown CPU gauge without a needle or pivot', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: { ...metrics(), cpu_percent: null } },
      global: { plugins: [pinia, i18n] },
    })

    const gauge = wrapper
      .findAll('[data-system-status-tile]')[0]!
      .find('[data-cpu-gauge]')
    expect(gauge.text()).toContain('--')
    expect(gauge.find('[data-cpu-gauge-unknown]').exists()).toBe(true)
    expect(gauge.find('[data-cpu-gauge-active]').exists()).toBe(false)
    expect(gauge.find('[data-cpu-gauge-needle]').exists()).toBe(false)
    expect(gauge.find('circle').exists()).toBe(false)
  })

  it('charts both bandwidth directions on one shared scale', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: metrics() },
      global: { plugins: [pinia, i18n] },
    })

    const spark = wrapper
      .findAll('[data-system-status-tile]')[2]!
      .find('[data-bandwidth-sparkline] svg[aria-hidden="true"]')
    expect(spark.exists()).toBe(true)
    // One line per direction plus the shaded download area beneath them.
    expect(spark.findAll('path[fill="none"]')).toHaveLength(2)
    expect(spark.findAll('circle')).toHaveLength(2)
  })

  it('does not advertise its own mock provenance', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: metrics() },
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.text()).not.toContain('Local mock')
    expect(wrapper.text()).not.toContain('demo data')
  })

  it('renders supplied metrics with resource gauges', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: metrics() },
      global: { plugins: [pinia, i18n] },
    })

    const text = wrapper.text()
    expect(text).toContain('34')
    expect(text).toContain('5.2 / 16')
    expect(wrapper.find('[data-disk-gauge]').text()).toContain('218')
    expect(wrapper.find('[data-disk-gauge]').text()).toContain('512')
    expect(text).toContain('99.7')
    expect(text).toContain('App traffic')
  })

  it('rounds CPU and app traffic values without truncating them', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          ...metrics(),
          cpu_percent: 4.627766599,
          bandwidth_up_mbps: 1.249,
          bandwidth_down_mbps: 6.555,
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    const cpuValue = wrapper
      .findAll('[data-system-status-tile]')[0]!
      .find('[data-cpu-gauge] span')
    expect(wrapper.text()).toContain('4.6%')
    expect(wrapper.text()).toContain('↑1.2 Mbps')
    expect(wrapper.text()).toContain('↓6.6 Mbps')
    expect(wrapper.text()).not.toContain('4.627766599')
    expect(cpuValue.classes()).toContain('whitespace-nowrap')
    expect(cpuValue.classes()).not.toContain('truncate')
  })

  it('formats each bandwidth direction with the smallest useful unit', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          ...metrics(),
          bandwidth_up_mbps: 0.45,
          bandwidth_down_mbps: 0.00042,
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    const bandwidth = wrapper.findAll('[data-system-status-tile]')[2]!
    expect(bandwidth.text()).toContain('↑450 Kbps')
    expect(bandwidth.text()).toContain('↓420 bps')
    expect(bandwidth.text()).not.toContain('Mbps')
    expect(bandwidth.findAll('[data-bandwidth-direction]')).toHaveLength(2)
  })

  it('clamps percentages and rejects invalid metric values', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          ...metrics(),
          cpu_percent: 140,
          memory_used_gb: -1,
          memory_total_gb: 0,
          disk_used_gb: Number.NaN,
          disk_total_gb: 100,
          api_success_rate: Number.POSITIVE_INFINITY,
          bandwidth_up_mbps: -0.5,
          bandwidth_down_mbps: Number.NaN,
          bandwidth_series: {
            up: [1, Number.NaN, 3],
            down: [2, 4, 6],
          },
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    const tiles = wrapper.findAll('[data-system-status-tile]')
    expect(tiles[0]!.text()).toContain('100%')
    expect(tiles[0]!.find('[data-cpu-gauge-active]').exists()).toBe(true)
    expect(tiles[1]!.text()).toContain('--')
    expect(tiles[2]!.text()).toContain('--')
    expect(tiles[2]!.find('[data-bandwidth-sparkline]').exists()).toBe(false)
    expect(tiles[3]!.text()).toContain('--')
    expect(wrapper.find('header').text()).toContain('No data')
  })

  it('keeps bandwidth values visible without drawing an incomplete series', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          ...metrics(),
          bandwidth_series: { up: [1], down: [2] },
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    const bandwidth = wrapper.findAll('[data-system-status-tile]')[2]!
    expect(bandwidth.text()).toContain('↑2.1 Mbps')
    expect(bandwidth.text()).toContain('↓12.4 Mbps')
    expect(bandwidth.find('[data-bandwidth-sparkline]').exists()).toBe(false)
  })

  it('reads storage in one unit with at most three significant figures', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: {
        metrics: {
          ...metrics(),
          memory_used_gb: 62.8,
          memory_total_gb: 125.7,
          disk_used_gb: 1843.2,
          disk_total_gb: 3726,
        },
      },
      global: { plugins: [pinia, i18n] },
    })

    const memory = wrapper.get('[data-metric="memory"]').text()
    const disk = wrapper.get('[data-metric="disk"]').text()
    expect(memory.replace(/\s+/g, '')).toContain('62.8/126GB')
    expect(disk).toContain('1.8')
    expect(disk).toContain('3.6')
    expect(disk).toContain('TB')
    expect(disk).not.toContain('GB')
  })

  it('uses the Chinese app traffic label', async () => {
    await setLocale('zh-CN')
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(SystemStatusCard, {
      props: { metrics: metrics() },
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.text()).toContain('应用流量')
  })
})
