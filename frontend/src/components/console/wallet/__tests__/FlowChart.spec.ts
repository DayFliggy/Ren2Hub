import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import type { ChartPalette } from '@/charts/palette'
import type { OptionBuilder } from '@/charts/useEChart'
import FlowChart from '@/components/console/wallet/FlowChart.vue'
import type { FlowPoint } from '@/composables/useDashboard'
import i18n, { loadMessageDomain } from '@/i18n'

let buildOption: OptionBuilder | undefined

vi.mock('@/charts/useEChart', () => ({
  useEChart: (_el: unknown, build: OptionBuilder) => {
    buildOption = build
    return { refresh: vi.fn(), dispatch: vi.fn() }
  },
}))

interface FlowChartOption {
  tooltip: { trigger: string }
  xAxis: {
    data: string[]
    axisLabel: {
      interval: string
      hideOverlap: boolean
      formatter: (value: string) => string
    }
  }
  series: Array<{ data: number[] }>
}

const palette = {
  signalStrong: '#74765a',
  surfaceSolid: '#fffdf8',
  borderSubtle: '#eeeeee',
  textPrimary: '#111111',
  textTertiary: '#333333',
  isDark: false,
} as ChartPalette

beforeAll(async () => {
  await loadMessageDomain('console')
})

describe('FlowChart', () => {
  it('shortens responsive ticks without losing full tooltip dates or amounts', () => {
    const flow: FlowPoint[] = Array.from({ length: 30 }, (_, index) => ({
      date: `2026-07-${String(index + 1).padStart(2, '0')}`,
      consume: String((index + 1) * 500_000),
      requests: index + 1,
    }))
    mount(FlowChart, { props: { flow }, global: { plugins: [i18n] } })

    const option = buildOption!(palette) as unknown as FlowChartOption
    expect(option.xAxis.axisLabel.formatter('2026-07-01')).toBe('07-01')
    expect(option.xAxis.axisLabel.interval).toBe('auto')
    expect(option.xAxis.axisLabel.hideOverlap).toBe(true)
    expect(option.tooltip.trigger).toBe('axis')
    expect(option.xAxis.data).toEqual(flow.map((point) => point.date))
    expect(option.series[0]!.data).toEqual(
      Array.from({ length: 30 }, (_, index) => index + 1)
    )
  })

  it('retains provided short labels and refreshes categories from new data', async () => {
    const wrapper = mount(FlowChart, {
      props: { flow: [] },
      global: { plugins: [i18n] },
    })
    await wrapper.setProps({
      flow: [{ date: '07-28', consume: '1000000', requests: 1 }],
    })

    const option = buildOption!(palette) as unknown as FlowChartOption
    expect(option.xAxis.axisLabel.formatter('07-28')).toBe('07-28')
    expect(option.xAxis.data).toEqual(['07-28'])
    expect(option.series[0]!.data).toEqual([2])
  })
})
