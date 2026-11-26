<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { tooltipStyle } from '@/charts/themePreset'
import { useEChart } from '@/charts/useEChart'
import { areaGradient, lineMood } from '@/charts/themePreset'

/** Palette role of a series; the CSS token keeps the legend in step with the canvas. */
const SERIES_COLORS = {
  accent: 'var(--accent)',
  signal: 'var(--signal)',
  glow: 'var(--glow)',
  support: 'var(--support)',
  trendCpu: 'var(--trend-cpu)',
  trendMemory: 'var(--trend-memory)',
  trendDisk: 'var(--trend-disk)',
  info: 'var(--status-info)',
  warning: 'var(--status-warning)',
  danger: 'var(--status-danger)',
  textPrimary: 'var(--text-primary)',
  textTertiary: 'var(--text-tertiary)',
} as const

export interface OpsTrendSeries {
  name: string
  /** Index into the live chart palette; follows the theme. */
  color: keyof typeof SERIES_COLORS
  points: Array<{ ts: number; value: number | null }>
  /** Overrides chart-level kind for mixed bar+line dual-axis charts. */
  kind?: 'line' | 'bar'
  /** Dashed line stroke, e.g. outbound traffic matching SystemStatusCard. */
  dashed?: boolean
  /** 0 = left y-axis (default), 1 = right secondary y-axis. */
  yAxisIndex?: 0 | 1
  /** Optional per-series tooltip/summary unit override. */
  unit?: string
}

export interface OpsSecondaryAxis {
  unit?: string
  max?: number | null
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
    secondaryAxis?: OpsSecondaryAxis | null
    gridLeft?: number
    gridRight?: number
    height?: number
  }>(),
  {
    kind: 'line',
    unit: '',
    threshold: null,
    max: null,
    secondaryAxis: null,
    gridLeft: 40,
    gridRight: undefined,
    height: 140,
  }
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

function seriesUnit(line: OpsTrendSeries) {
  if (line.unit !== undefined) return line.unit
  if (line.yAxisIndex === 1 && props.secondaryAxis?.unit !== undefined)
    return props.secondaryAxis.unit
  return props.unit
}

const summary = computed(() =>
  props.series
    .map((line) => {
      const latest = [...line.points]
        .reverse()
        .find((point) => point.value !== null)
      return `${line.name}: ${latest ? axisNumber(latest.value!) : '--'}${seriesUnit(line)}`
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
    const hasSecondary = Boolean(props.secondaryAxis)
    const effectiveRight = props.gridRight ?? (hasSecondary ? 44 : 8)
    const lineCount = props.series.filter(
      (line) => (line.kind ?? props.kind) === 'line'
    ).length
    const filled = props.series.length === 1 && lineCount === 1
    const lines = props.series.map((line) => {
      const seriesKind = line.kind ?? props.kind
      const color = palette[line.color]
      const unit = seriesUnit(line)
      const shape =
        seriesKind === 'bar'
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
      const singlePoint =
        line.points.filter((point) => point.value !== null).length === 1
      return {
        name: line.name,
        type: seriesKind,
        yAxisIndex: hasSecondary ? (line.yAxisIndex ?? 0) : 0,
        data: withGaps(line.points, props.bucketSeconds),
        showSymbol: singlePoint,
        symbol: 'circle',
        symbolSize: 4 * scale,
        smooth: mood.line.smooth,
        connectNulls: false,
        itemStyle: { color },
        tooltip: {
          valueFormatter: (value: unknown) =>
            typeof value === 'number' ? `${axisNumber(value)}${unit}` : '--',
        },
        lineStyle: {
          width: (line.dashed ? 1.5 : 1.75) * scale,
          type: line.dashed ? ('dashed' as const) : ('solid' as const),
          color,
          ...(filled && palette.isDark
            ? {
                shadowBlur: mood.line.lineStyle.shadowBlur,
                shadowColor: mood.line.lineStyle.shadowColor,
              }
            : { shadowBlur: 0, shadowColor: 'transparent' }),
        },
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
    const primaryYAxis = {
      type: 'value' as const,
      min: 0,
      ...(props.max === null
        ? {}
        : { max: props.max, interval: props.max / 4 }),
      splitNumber: 3,
      axisLabel: {
        color: palette.textTertiary,
        fontSize: 10 * scale,
        formatter: (value: number) => axisNumber(value),
      },
      splitLine: mood.splitLine,
    }
    const yAxis = hasSecondary
      ? [
          primaryYAxis,
          {
            type: 'value' as const,
            min: 0,
            ...(props.secondaryAxis?.max == null
              ? {}
              : {
                  max: props.secondaryAxis.max,
                  interval: props.secondaryAxis.max / 4,
                }),
            splitNumber: 3,
            axisLabel: {
              color: palette.textTertiary,
              fontSize: 10 * scale,
              formatter: (value: number) =>
                `${axisNumber(value)}${props.secondaryAxis?.unit ?? ''}`,
            },
            splitLine: { show: false },
          },
        ]
      : primaryYAxis
    return {
      animation: false,
      grid: {
        top: 12 * scale,
        right: effectiveRight * scale,
        bottom: 22 * scale,
        left: props.gridLeft * scale,
      },
      tooltip: {
        trigger: 'axis',
        ...tooltipStyle(palette, scale),
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
      yAxis,
      series: [...lines, ...threshold],
    }
  },
  [
    () => props.series,
    () => props.threshold,
    () => props.range,
    () => props.max,
    () => props.secondaryAxis,
    () => props.gridLeft,
    () => props.gridRight,
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
          v-if="line.dashed"
          class="ops-line-swatch ops-line-swatch--dashed"
          :style="{ borderColor: SERIES_COLORS[line.color] }"
        /><span
          v-else
          class="ops-dot"
          :style="{ background: SERIES_COLORS[line.color] }"
        />{{ line.name }}
      </li>
    </ul>
    <div ref="el" :style="{ height: `${height / 16}rem` }" aria-hidden="true" />
  </figure>
</template>
