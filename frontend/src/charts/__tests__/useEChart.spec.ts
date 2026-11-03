import { defineComponent, ref, type PropType, type WatchSource } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const echartsMock = vi.hoisted(() => {
  const instances: Array<{
    setOption: ReturnType<typeof vi.fn>
    resize: ReturnType<typeof vi.fn>
    dispose: ReturnType<typeof vi.fn>
    dispatchAction: ReturnType<typeof vi.fn>
  }> = []
  return { instances }
})

vi.mock('echarts/core', () => ({
  use: vi.fn(),
  init: vi.fn(() => {
    const instance = {
      setOption: vi.fn(),
      resize: vi.fn(),
      dispose: vi.fn(),
      dispatchAction: vi.fn(),
    }
    echartsMock.instances.push(instance)
    return instance
  }),
}))

vi.mock('echarts/charts', () => ({
  BarChart: {},
  LineChart: {},
  PieChart: {},
}))
vi.mock('echarts/components', () => ({
  GraphicComponent: {},
  GridComponent: {},
  LegendComponent: {},
  TooltipComponent: {},
}))
vi.mock('echarts/renderers', () => ({ CanvasRenderer: {} }))

import { useTheme } from '@/composables/useTheme'
import { useEChart, type OptionBuilder } from '@/charts/useEChart'

const Host = defineComponent({
  props: {
    loading: Boolean,
    optionBuilder: {
      type: Function as PropType<OptionBuilder>,
      default: () => ({ series: [] }),
    },
    watchSource: {
      type: [Object, Array, Function] as PropType<WatchSource | WatchSource[]>,
      default: undefined,
    },
  },
  setup(props) {
    const chartEl = ref<HTMLElement | null>(null)
    useEChart(
      chartEl,
      (palette, scale) => props.optionBuilder(palette, scale),
      props.watchSource
    )
    return { chartEl }
  },
  template: '<div><div v-if="!loading" ref="chartEl" class="chart" /></div>',
})

const resizeCallbacks: Array<() => void> = []
let previousFontSize = ''

beforeEach(() => {
  echartsMock.instances.splice(0)
  resizeCallbacks.splice(0)
  previousFontSize = document.documentElement.style.fontSize
  document.documentElement.style.fontSize = '16px'
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback: ResizeObserverCallback) {
        resizeCallbacks.push(() =>
          callback([], this as unknown as ResizeObserver)
        )
      }
      observe() {}
      disconnect() {}
    }
  )
})

afterEach(() => {
  document.documentElement.style.fontSize = previousFontSize
  vi.unstubAllGlobals()
})

describe('useEChart', () => {
  it('disposes and reinitializes when a loading branch replaces the host DOM', async () => {
    const wrapper = mount(Host, { props: { loading: false } })
    await flushPromises()

    expect(echartsMock.instances).toHaveLength(1)
    const first = echartsMock.instances[0]!
    expect(first.setOption).toHaveBeenCalledOnce()

    await wrapper.setProps({ loading: true })
    await flushPromises()
    expect(first.dispose).toHaveBeenCalledOnce()

    await wrapper.setProps({ loading: false })
    await flushPromises()
    expect(echartsMock.instances).toHaveLength(2)
    expect(echartsMock.instances[1]!.setOption).toHaveBeenCalledOnce()

    wrapper.unmount()
    expect(echartsMock.instances[1]!.dispose).toHaveBeenCalledOnce()
  })

  it('resizes without rebuilding options when the layout scale is unchanged', async () => {
    const wrapper = mount(Host)
    await flushPromises()
    const chart = echartsMock.instances[0]!

    resizeCallbacks[0]!()
    resizeCallbacks[0]!()

    expect(chart.resize).toHaveBeenCalledTimes(2)
    expect(chart.setOption).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('rebuilds pixel sizes when the root font changes and keeps the chart data', async () => {
    const wrapper = mount(Host, {
      props: {
        optionBuilder: (_palette, scale = 1) => ({
          textStyle: { fontSize: 12 * scale },
          series: [{ type: 'line', data: [10, 20] }],
        }),
      },
    })
    await flushPromises()
    const chart = echartsMock.instances[0]!
    const optionAt = (fontSize: number) =>
      expect.objectContaining({
        textStyle: { fontSize },
        series: [{ type: 'line', data: [10, 20] }],
      })

    expect(chart.setOption).toHaveBeenLastCalledWith(optionAt(12), true)
    document.documentElement.style.fontSize = '20px'
    resizeCallbacks[0]!()
    expect(chart.setOption).toHaveBeenLastCalledWith(optionAt(15), true)
    expect(chart.setOption).toHaveBeenCalledTimes(2)

    // A second resize at the same scale keeps legend selection and zoom state.
    resizeCallbacks[0]!()
    expect(chart.setOption).toHaveBeenCalledTimes(2)
    document.documentElement.style.fontSize = '18px'
    resizeCallbacks[0]!()
    expect(chart.setOption).toHaveBeenLastCalledWith(optionAt(13.5), true)
    document.documentElement.style.fontSize = '16px'
    resizeCallbacks[0]!()
    expect(chart.setOption).toHaveBeenLastCalledWith(optionAt(12), true)
    expect(chart.resize).toHaveBeenCalledTimes(4)
    wrapper.unmount()
  })

  it('differentiates theme incremental update (notMerge: false, no animation) from data replacement (notMerge: true)', async () => {
    const dataRef = ref([10, 20])
    const theme = useTheme()
    const oldPreference = theme.preference.value
    theme.setThemePreference('light')
    await flushPromises()

    const wrapper = mount(Host, {
      props: {
        optionBuilder: (palette) => ({
          textStyle: { color: palette.textPrimary },
          series: [{ type: 'line', data: dataRef.value }],
        }),
        watchSource: dataRef,
      },
    })
    await flushPromises()
    const chart = echartsMock.instances[0]!

    // Initial attach uses notMerge = true
    expect(chart.setOption).toHaveBeenLastCalledWith(
      expect.objectContaining({ series: [{ type: 'line', data: [10, 20] }] }),
      true
    )

    // Data change replaces options with notMerge = true
    dataRef.value = [30, 40]
    await flushPromises()
    expect(chart.setOption).toHaveBeenLastCalledWith(
      expect.objectContaining({ series: [{ type: 'line', data: [30, 40] }] }),
      true
    )

    // Theme flip updates options incrementally with notMerge = false and animation: false
    theme.setThemePreference('dark')
    await flushPromises()
    expect(chart.setOption).toHaveBeenLastCalledWith(
      expect.objectContaining({ animation: false }),
      false
    )

    wrapper.unmount()
    theme.setThemePreference(oldPreference)
  })
})

it('rebuilds chart labels, tooltip and active colors on theme changes and stops after disposal', async () => {
  const root = document.documentElement
  const theme = useTheme()
  const oldPreference = theme.preference.value
  const oldTheme = root.dataset.theme
  const oldColor = root.style.getPropertyValue('--text-primary')
  root.style.setProperty('--text-primary', '#112233')
  theme.setThemePreference('light')
  await flushPromises()

  const wrapper = mount(Host, {
    props: {
      optionBuilder: (palette) => ({
        textStyle: { color: palette.textPrimary },
        tooltip: { textStyle: { color: palette.textPrimary } },
        legend: {
          selected: { 'Model A': true, 'Model B': false },
        },
        series: [
          {
            type: 'line',
            name: 'Model A',
            data: [1, 2],
            emphasis: { label: { color: palette.textPrimary } },
          },
          {
            type: 'line',
            name: 'Model B',
            data: [3, 4],
            emphasis: { label: { color: palette.textPrimary } },
          },
        ],
      }),
    },
  })
  try {
    await flushPromises()
    const chart = echartsMock.instances[0]!
    expect(chart.setOption.mock.lastCall?.[0]).toMatchObject({
      textStyle: { color: '#112233' },
    })
    // Initial render uses notMerge = true
    expect(chart.setOption.mock.lastCall?.[1]).toBe(true)

    root.style.setProperty('--text-primary', '#ddeeff')
    theme.setThemePreference('dark')
    await flushPromises()

    expect(chart.setOption.mock.lastCall?.[0]).toMatchObject({
      animation: false,
      textStyle: { color: '#ddeeff' },
      tooltip: { textStyle: { color: '#ddeeff' } },
      series: [
        { emphasis: { label: { color: '#ddeeff' } } },
        { emphasis: { label: { color: '#ddeeff' } } },
      ],
    })
    // Theme switch uses incremental update (notMerge = false) to preserve legend/zoom states
    expect(chart.setOption.mock.lastCall?.[1]).toBe(false)

    root.style.setProperty('--text-primary', '#112233')
    theme.setThemePreference('light')
    await flushPromises()

    expect(chart.setOption.mock.lastCall?.[0]).toMatchObject({
      animation: false,
      textStyle: { color: '#112233' },
    })
    expect(chart.setOption.mock.lastCall?.[1]).toBe(false)

    wrapper.unmount()
    const calls = chart.setOption.mock.calls.length
    theme.setThemePreference('dark')
    await flushPromises()
    expect(chart.setOption).toHaveBeenCalledTimes(calls)
  } finally {
    theme.setThemePreference(oldPreference)
    if (oldTheme === undefined) delete root.dataset.theme
    else root.dataset.theme = oldTheme
    if (oldColor) root.style.setProperty('--text-primary', oldColor)
    else root.style.removeProperty('--text-primary')
    if (wrapper.exists()) wrapper.unmount()
  }
})
