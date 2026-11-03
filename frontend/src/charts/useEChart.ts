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

import { useTheme } from '@/composables/useTheme'
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
  let renderedScale = 1
  const { resolvedTheme } = useTheme()

  function layoutScale() {
    const fontSize = getComputedStyle(document.documentElement).fontSize
    return fontSize ? Number.parseFloat(fontSize) / 16 : 1
  }

  function render(opts?: { isThemeUpdate?: boolean }) {
    if (!chart) return
    renderedScale = layoutScale()
    const option = buildOption(chartPalette(), renderedScale)
    const isThemeUpdate = Boolean(opts?.isThemeUpdate)
    const prefersReducedMotion = window.matchMedia(
      '(prefers-reduced-motion: reduce)'
    ).matches

    if (isThemeUpdate) {
      // Incremental theme update: do not reset legend/interaction state, flush immediately without animation
      chart.setOption({ ...option, animation: false }, false)
    } else {
      // Data or scale updates preserve existing full replace semantics
      chart.setOption(
        prefersReducedMotion ? { ...option, animation: false } : option,
        true
      )
    }
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

  // Watch resolvedTheme post-flush so documentElement tokens and classes are applied.
  watch(
    resolvedTheme,
    () => {
      render({ isThemeUpdate: true })
    },
    { flush: 'post' }
  )

  function detach() {
    resizeObserver?.disconnect()
    resizeObserver = null
    chart?.dispose()
    chart = null
    chartTarget = null
  }

  function attach(target: HTMLElement) {
    if (chart && chartTarget === target) return
    detach()
    chartTarget = target
    chart = echarts.init(target)
    render()

    resizeObserver = new ResizeObserver(() => {
      chart?.resize()
      // ECharts' pixel options do not inherit the rem size of the host.
      // Rebuild only when that size changes, preserving interaction state on
      // ordinary container resizes such as a sidebar toggle.
      if (layoutScale() !== renderedScale) render()
    })
    resizeObserver.observe(target)
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
