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

export type OptionBuilder = (
  palette: ChartPalette,
  scale?: number
) => EChartsCoreOption

/**
 * Binds an ECharts instance to an element. The option is built from the
 * live semantic palette and rebuilt whenever the resolved theme flips
 * (html[data-theme], written by useTheme) or any of `watchSources` changes
 * (async data / interactive toggles). Sources are watched shallowly — pass
 * wholesale-replaced refs/computeds/getters, not in-place-mutated arrays.
 */
export function useEChart(
  el: Ref<HTMLElement | null>,
  buildOption: OptionBuilder,
  watchSources?: WatchSource | WatchSource[]
) {
  let chart: echarts.ECharts | null = null
  let chartTarget: HTMLElement | null = null
  let resizeObserver: ResizeObserver | null = null
  let themeObserver: MutationObserver | null = null
  let renderedScale = 1

  function layoutScale() {
    const fontSize = getComputedStyle(document.documentElement).fontSize
    return fontSize ? Number.parseFloat(fontSize) / 16 : 1
  }

  function render() {
    if (!chart || !chartTarget) return
    renderedScale = layoutScale()
    const built = buildOption(chartPalette(), renderedScale)
    const option = {
      ...built,
      textStyle: {
        fontFamily: getComputedStyle(chartTarget).fontFamily,
        ...built.textStyle,
      },
    }
    chart.setOption(
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
        ? { ...option, animation: false }
        : option,
      true
    )
  }

  /**
   * Forwards an action to the chart, e.g. highlighting a slice when the row it
   * corresponds to is hovered. No-ops before mount and after dispose.
   */
  function dispatch(action: Parameters<echarts.ECharts['dispatchAction']>[0]) {
    chart?.dispatchAction(action)
  }

  if (watchSources) {
    // Shallow on purpose: every call site replaces its source wholesale, and a
    // deep walk over full chart datasets on each change is wasted work.
    watch(watchSources, () => render())
  }

  function detach() {
    resizeObserver?.disconnect()
    resizeObserver = null
    themeObserver?.disconnect()
    themeObserver = null
    document.fonts?.removeEventListener('loadingdone', render)
    chart?.dispose()
    chart = null
    chartTarget = null
  }

  function attach(target: HTMLElement) {
    if (chart && chartTarget === target) return
    detach()
    chartTarget = target
    chart = echarts.init(target)
    // Canvas text does not inherit CSS fonts or redraw when a web font loads.
    document.fonts?.addEventListener('loadingdone', render)
    render()

    resizeObserver = new ResizeObserver(() => {
      chart?.resize()
      // ECharts' pixel options do not inherit the rem size of the host.
      // Rebuild only when that size changes, preserving interaction state on
      // ordinary container resizes such as a sidebar toggle.
      if (layoutScale() !== renderedScale) render()
    })
    resizeObserver.observe(target)

    // data-theme changes only on light/dark flips (useTheme), unlike `class`,
    // which any route/overlay toggle can touch — observing it avoids full
    // chart rebuilds on unrelated class churn.
    themeObserver = new MutationObserver(() => render())
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    })
  }

  onMounted(() => {
    if (el.value) attach(el.value)
  })

  // A v-if loading skeleton replaces the chart host element. Dispose the
  // instance bound to the removed node before attaching the replacement, or
  // ECharts keeps rendering into a detached DOM subtree.
  watch(
    el,
    (target) => {
      if (target === chartTarget) return
      detach()
      if (target) attach(target)
    },
    { flush: 'post' }
  )

  onBeforeUnmount(() => {
    detach()
  })

  return { refresh: render, dispatch }
}
