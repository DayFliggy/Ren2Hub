import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import {
  measuredResource,
  performanceFixture,
} from '@/api/__tests__/opsFixtures'
import { projectOpsOverview } from '@/api/opsMonitor'
import MiniRing from '@/components/console/dashboard/MiniRing.vue'
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
          request_mbps: null,
          response_mbps: null,
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
