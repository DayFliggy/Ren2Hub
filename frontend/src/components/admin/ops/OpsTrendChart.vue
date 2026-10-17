<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useEChart } from '@/charts/useEChart'
import { areaGradient, lineMood } from '@/charts/themePreset'

/** Palette role of a series; the CSS token keeps the legend in step with the canvas. */
const SERIES_COLORS = {
  accent: 'var(--accent)',
  signal: 'var(--signal)',
  glow: 'var(--glow)',
  support: 'var(--support)',
  info: 'var(--status-info)',
  warning: 'var(--status-warning)',
  danger: 'var(--status-danger)',
} as const

export interface OpsTrendSeries {
  name: string
  /** Index into the live chart palette; follows the theme. */
  color: keyof typeof SERIES_COLORS
  points: Array<{ ts: number; value: number | null }>
}

/**
 * Crisp time-series chart for the ops monitor. A missing bucket (a gap longer
 * than 1.5 buckets) breaks the line instead of being interpolated, and an
 * optional threshold renders as a dashed reference line. Several series get a
 * legend and plain lines, because stacked area fills hide each other.
 */
const props = withDefaults(
  defineProps<{
    label: string
    series: OpsTrendSeries[]
    bucketSeconds: number
    /** Visible time range in Unix seconds, so a fresh process shows its gap. */
    range: [number, number]
    kind?: 'line' | 'bar'
    unit?: string
    threshold?: number | null
    /** Fixed y-axis ceiling, e.g. 100 for percentages; auto-scaled when null. */
    max?: number | null
    height?: number
  }>(),
  { kind: 'line', unit: '', threshold: null, max: null, height: 140 }
)
const { locale } = useI18n()
const el = ref<HTMLElement | null>(null)

function withGaps(points: OpsTrendSeries['points'], bucketSeconds: number) {
  const data: Array<[number, number | null]> = []
  points.forEach((point, i) => {
    const previous = points[i - 1]
    if (previous && point.ts - previous.ts > bucketSeconds * 1.5)
      data.push([(previous.ts + bucketSeconds) * 1000, null])
    data.push([point.ts * 1000, point.value])
  })
  return data
}

function axisNumber(value: number) {
  return new Intl.NumberFormat(locale.value, {
    notation: value >= 10_000 ? 'compact' : 'standard',
    maximumFractionDigits: value < 10 ? 1 : 0,
  }).format(value)
}

const summary = computed(() =>
  props.series
    .map((line) => {
      const latest = [...line.points]
        .reverse()
        .find((point) => point.value !== null)
      return `${line.name}: ${latest ? axisNumber(latest.value!) : '--'}${props.unit}`
    })
    .join('; ')
)

useEChart(
  el,
  (palette, scale = 1) => {
    const mood = lineMood(palette)
    const clock = new Intl.DateTimeFormat(locale.value, {
      hour: '2-digit',
      minute: '2-digit',
    })
    const filled = props.kind === 'line' && props.series.length === 1
    const lines = props.series.map((line) => {
      const color = palette[line.color]
      const shape =
        props.kind === 'bar'
          ? {
              barMaxWidth: 10 * scale,
              itemStyle: {
                color,
                borderRadius: mood.barRadius.map((radius) => radius * scale),
              },
            }
          : filled
            ? {
                areaStyle: {
                  color: {
                    type: 'linear' as const,
                    x: 0,
                    y: 0,
                    x2: 0,
                    y2: 1,
                    colorStops: areaGradient(palette, color),
                  },
                },
              }
            : {}
      return {
        name: line.name,
        type: props.kind,
        data: withGaps(line.points, props.bucketSeconds),
        showSymbol: false,
        symbol: 'circle',
        symbolSize: 4 * scale,
        smooth: mood.line.smooth,
        connectNulls: false,
        itemStyle: { color },
        lineStyle: { width: 1.75 * scale, color },
        ...shape,
      }
    })
    const threshold =
      props.threshold && props.threshold > 0
        ? [
            {
              name: 'threshold',
              type: 'line',
              data: [
                [props.range[0] * 1000, props.threshold],
                [props.range[1] * 1000, props.threshold],
              ],
              symbol: 'none',
              silent: true,
              tooltip: { show: false },
              lineStyle: {
                width: 1 * scale,
                type: 'dashed',
                color: palette.warning,
              },
            },
          ]
        : []
    return {
      animation: false,
      grid: {
        top: 12 * scale,
        right: 8 * scale,
        bottom: 22 * scale,
        left: 40 * scale,
      },
      tooltip: {
        trigger: 'axis',
        backgroundColor: palette.surfaceSolid,
        borderColor: palette.borderSubtle,
        textStyle: { color: palette.textPrimary, fontSize: 12 * scale },
        valueFormatter: (value: unknown) =>
          typeof value === 'number'
            ? `${axisNumber(value)}${props.unit}`
            : '--',
      },
      xAxis: {
        type: 'time',
        min: props.range[0] * 1000,
        max: props.range[1] * 1000,
        axisLine: { lineStyle: { color: palette.borderSubtle } },
        axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: {
          color: palette.textTertiary,
          fontSize: 10 * scale,
          hideOverlap: true,
          formatter: (value: number) => clock.format(value),
        },
      },
      yAxis: {
        type: 'value',
        min: 0,
        // A fixed ceiling gets even quarter ticks, so its top value is labelled.
        ...(props.max === null
          ? {}
          : { max: props.max, interval: props.max / 4 }),
        splitNumber: 3,
        axisLabel: {
          color: palette.textTertiary,
          fontSize: 10 * scale,
          formatter: (value: number) => axisNumber(value),
        },
        splitLine: { lineStyle: { type: 'solid', color: palette.chartGrid } },
      },
      series: [...lines, ...threshold],
    }
  },
  [
    () => props.series,
    () => props.threshold,
    () => props.range,
    () => props.max,
    locale,
  ]
)
</script>

<template>
  <figure class="ops-chart">
    <figcaption class="sr-only">{{ label }}: {{ summary }}</figcaption>
    <ul v-if="series.length > 1" class="ops-legend" aria-hidden="true">
      <li v-for="line in series" :key="line.name">
        <span
          class="ops-dot"
          :style="{ background: SERIES_COLORS[line.color] }"
        />{{ line.name }}
      </li>
    </ul>
    <div ref="el" :style="{ height: `${height / 16}rem` }" aria-hidden="true" />
  </figure>
</template>
