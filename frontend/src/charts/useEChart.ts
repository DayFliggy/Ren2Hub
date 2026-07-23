import {
  onBeforeUnmount,
  onMounted,
  watch,
  type Ref,
  type WatchSource,
} from 'vue'

import { BarChart, LineChart, PieChart } from 'echarts/charts'
import {
  GraphicComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from 'echarts/components'
import * as echarts from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsCoreOption } from 'echarts/core'

import { chartPalette, type ChartPalette } from './palette'

echarts.use([
  LineChart,
  PieChart,
  BarChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  GraphicComponent,
  CanvasRenderer,
])

export type OptionBuilder = (palette: ChartPalette) => EChartsCoreOption

/**
 * Binds an ECharts instance to an element. The option is built from the
 * live semantic palette and rebuilt whenever html.dark flips (theme switch)
 * or any of `watchSources` changes (async data / interactive toggles).
 */
export function useEChart(
  el: Ref<HTMLElement | null>,
  buildOption: OptionBuilder,
  watchSources?: WatchSource | WatchSource[]
) {
  let chart: echarts.ECharts | null = null
  let resizeObserver: ResizeObserver | null = null
  let themeObserver: MutationObserver | null = null

  function render() {
    if (!chart) return
    chart.setOption(buildOption(chartPalette()), true)
  }

  if (watchSources) {
    watch(watchSources, () => render(), { deep: true })
  }

  onMounted(() => {
    if (!el.value) return
    chart = echarts.init(el.value)
    render()

    resizeObserver = new ResizeObserver(() => chart?.resize())
    resizeObserver.observe(el.value)

    themeObserver = new MutationObserver(() => render())
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
  })

  onBeforeUnmount(() => {
    resizeObserver?.disconnect()
    themeObserver?.disconnect()
    chart?.dispose()
    chart = null
  })

  return { refresh: render }
}
