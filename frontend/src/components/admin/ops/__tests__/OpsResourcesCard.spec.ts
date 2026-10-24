import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import { performanceFixture } from '@/api/__tests__/opsFixtures'
import { projectOpsOverview } from '@/api/opsMonitor'
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
