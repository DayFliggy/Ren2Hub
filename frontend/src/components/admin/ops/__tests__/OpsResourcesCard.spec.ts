import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import {
  measuredResource,
  performanceFixture,
} from '@/api/__tests__/opsFixtures'
import type { Observability } from '@/api/observability'
import { projectOpsOverview } from '@/api/opsMonitor'
import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import OpsMetric from '../OpsMetric.vue'
import OpsResourcesCard from '../OpsResourcesCard.vue'
import OpsTrendChart from '../OpsTrendChart.vue'

vi.mock('@/charts/useEChart', () => ({ useEChart: vi.fn() }))

describe('resource history scale', () => {
  it.each([40, 150])(
    'keeps a %s%% CPU reading within the chart and preserves missing samples',
    (cpu) => {
      const stats = performanceFixture()
      stats.observability.series.points = [
        {
          ts: 3600,
          cpu,
          memory: null,
          disk: 40,
          inbound_mbps: null,
          outbound_mbps: null,
          goroutines: null,
          heap_alloc_bytes: null,
        },
      ]
      const wrapper = mount(OpsResourcesCard, {
        props: {
          overview: projectOpsOverview(stats, null, '1h', [], 3600),
          loading: false,
          error: '',
          updatedAt: 3600000,
        },
        global: {
          plugins: [createI18n({ legacy: false, locale: 'en' })],
        },
      })
      const chart = wrapper.findAllComponents(OpsTrendChart)[0]!
      expect(chart.props('max')).toBe(Math.max(100, cpu))
      expect(chart.props('series')[0]!.points[0]!.value).toBe(cpu)
      expect(chart.props('series')[1]!.points[0]!.value).toBeNull()
      wrapper.unmount()
    }
  )
})

describe('resource gauge colours', () => {
  /** The same reading and verdict take the dashboard tile's tier here too. */
  it('colours each gauge by the dashboard load tier', () => {
    const stats = performanceFixture()
    const resources = stats.observability.resources
    resources.cpu = measuredResource(72)
    resources.memory = measuredResource(93)
    resources.disk = { ...resources.disk, percent: 99, threshold: 0 }
    stats.observability.checks = [
      { id: 'cpu', status: 'ok', reason: '' },
      { id: 'memory', status: 'warning', reason: 'threshold_exceeded' },
      { id: 'disk', status: 'disabled', reason: 'disabled' },
    ]
    const wrapper = mount(OpsResourcesCard, {
      props: {
        overview: projectOpsOverview(stats, null, '1h', [], 3600),
        loading: false,
        error: '',
        updatedAt: 3600000,
      },
      global: { plugins: [createI18n({ legacy: false, locale: 'en' })] },
    })
    const gauge = (key: string) => wrapper.get(`[data-ops-gauge="${key}"]`)
    expect(gauge('cpu').attributes('data-load-tier')).toBe('notice')
    expect(gauge('memory').attributes('data-load-tier')).toBe('warning')
    expect(gauge('disk').attributes('data-load-tier')).toBe('unchecked')
    const rings = wrapper.findAllComponents(MiniRing)
    expect(rings.map((ring) => ring.props('color'))).toEqual([
      'var(--load-notice)',
      'var(--load-warning)',
      'var(--load-unchecked)',
    ])
    wrapper.unmount()
  })
  it('leaves a stale reading uncoloured', () => {
    const stats = performanceFixture()
    stats.observability.resources.cpu = {
      ...measuredResource(0),
      available: false,
      percent: null,
      state: 'stale',
    }
    stats.observability.checks = [
      { id: 'cpu', status: 'stale', reason: 'stale' },
    ]
    const wrapper = mount(OpsResourcesCard, {
      props: {
        overview: projectOpsOverview(stats, null, '1h', [], 3600),
        loading: false,
        error: '',
        updatedAt: 3600000,
      },
      global: { plugins: [createI18n({ legacy: false, locale: 'en' })] },
    })
    expect(
      wrapper.get('[data-ops-gauge="cpu"]').attributes('data-load-tier')
    ).toBe('unknown')
    const ring = wrapper.findAllComponents(MiniRing)[0]!
    expect(ring.props('indeterminate')).toBe(true)
    expect(ring.props('color')).toBe('var(--text-tertiary)')
    wrapper.unmount()
  })
})

describe('traffic readings', () => {
  function mountTraffic(
    traffic: Partial<Observability['traffic']>,
    locale = 'en',
    edit: (stats: ReturnType<typeof performanceFixture>) => void = () => {}
  ) {
    const stats = performanceFixture()
    Object.assign(stats.observability.traffic, traffic)
    edit(stats)
    return mount(OpsResourcesCard, {
      props: {
        overview: projectOpsOverview(stats, null, '1h', [], 3600),
        loading: false,
        error: '',
        updatedAt: 3600000,
      },
      global: { plugins: [createI18n({ legacy: false, locale })] },
    })
  }
  const metric = (wrapper: ReturnType<typeof mountTraffic>, label: string) =>
    wrapper
      .findAllComponents(OpsMetric)
      .find((node) => node.props('label') === label)!

  it('shows outbound and inbound the same way in the figures and the lines', () => {
    const wrapper = mountTraffic(
      { inbound_mbps: 1, outbound_mbps: 7 },
      'en',
      (stats) => {
        stats.observability.series.points = [0, 1].map((i) => ({
          ...stats.observability.series.points[i]!,
          inbound_mbps: 1,
          outbound_mbps: 7,
        }))
      }
    )
    expect(metric(wrapper, 'Outbound').props('value')).toBe('7')
    expect(metric(wrapper, 'Inbound').props('value')).toBe('1')
    const chart = wrapper.findAllComponents(OpsTrendChart)[1]!
    expect(chart.props('label')).toBe('Traffic')
    const lines = Object.fromEntries(
      chart
        .props('series')
        .map((line) => [line.name, line.points.map((point) => point.value)])
    )
    expect(lines).toEqual({ Outbound: [7, 7], Inbound: [1, 1] })
    wrapper.unmount()
  })
  it('names the host interface it counts', () => {
    const wrapper = mountTraffic({})
    const note = wrapper.get('[data-ops-traffic]')
    expect(note.attributes('title')).toBe(
      'All traffic on host interface eth0; relayed requests count both client and upstream legs.'
    )
    expect(note.attributes('aria-description')).toBe(note.attributes('title'))
    wrapper.unmount()
    const zh = mountTraffic({}, 'zh-CN')
    expect(zh.get('[data-ops-traffic]').attributes('title')).toBe(
      '宿主机网卡 eth0 的全部收发；中转请求同时产生客户端和上游两段流量。'
    )
    zh.unmount()
  })
  it('describes the listener scope without an interface', () => {
    const wrapper = mountTraffic({
      scope: 'application_listener',
      interface: '',
    })
    expect(wrapper.get('[data-ops-traffic]').attributes('title')).toBe(
      "All bytes on the main service port, including headers, bodies, WebSocket and health checks; excludes the app's own upstream connections and the pprof port."
    )
    wrapper.unmount()
  })
  it('appends why a host reading is unavailable and keeps the figures empty', () => {
    const failure = {
      inbound_mbps: null,
      outbound_mbps: null,
      reason: 'host_interface_missing',
    } as const
    const wrapper = mountTraffic(failure)
    expect(wrapper.get('[data-ops-traffic]').attributes('title')).toBe(
      'All traffic on host interface eth0; relayed requests count both client and upstream legs. Reading unavailable: no eth0 entry in the host counters.'
    )
    expect(metric(wrapper, 'Outbound').props('value')).toBe('No data')
    expect(metric(wrapper, 'Inbound').props('value')).toBe('No data')
    wrapper.unmount()
    const zh = mountTraffic(failure, 'zh-CN')
    expect(zh.get('[data-ops-traffic]').attributes('title')).toBe(
      '宿主机网卡 eth0 的全部收发；中转请求同时产生客户端和上游两段流量。读数不可用：宿主机网卡计数中没有 eth0。'
    )
    expect(metric(zh, '出站').props('value')).toBe('暂无数据')
    zh.unmount()
  })
  it('leaves no blank interface name when host configuration is invalid', () => {
    const invalid = {
      interface: '',
      inbound_mbps: null,
      outbound_mbps: null,
      reason: 'host_config_invalid',
    } as const
    const wrapper = mountTraffic(invalid)
    expect(wrapper.get('[data-ops-traffic]').attributes('title')).toBe(
      'All traffic on the host interface; relayed requests count both client and upstream legs. Reading unavailable: invalid host traffic configuration.'
    )
    wrapper.unmount()
    const zh = mountTraffic(invalid, 'zh-CN')
    expect(zh.get('[data-ops-traffic]').attributes('title')).toBe(
      '宿主机网卡的全部收发；中转请求同时产生客户端和上游两段流量。读数不可用：宿主机流量配置无效。'
    )
    zh.unmount()
  })
})
