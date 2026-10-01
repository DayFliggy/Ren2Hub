<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useEChart } from '@/charts/useEChart'
import { lineMood } from '@/charts/themePreset'

export interface OpsTrendSeries {
  name: string
  /** Index into the live chart palette; follows the theme. */
  color: 'accent' | 'signal' | 'support' | 'info' | 'warning' | 'danger'
  points: Array<{ ts: number; value: number | null }>
}

/**
 * Crisp time-series chart for the ops monitor. A missing bucket (a gap longer
 * than 1.5 buckets) breaks the line instead of being interpolated, and an
 * optional threshold renders as a dashed reference line.
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
    height?: number
  }>(),
  { kind: 'line', unit: '', threshold: null, height: 140 }
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
  (palette) => {
    const mood = lineMood(palette)
    const clock = new Intl.DateTimeFormat(locale.value, {
      hour: '2-digit',
      minute: '2-digit',
    })
    const lines = props.series.map((line) => ({
      name: line.name,
      type: props.kind,
      data: withGaps(line.points, props.bucketSeconds),
      showSymbol: false,
      symbol: 'circle',
      symbolSize: 4,
      smooth: false,
      connectNulls: false,
      itemStyle: { color: palette[line.color] },
      lineStyle: { width: 1.75, color: palette[line.color] },
      ...(props.kind === 'bar'
        ? {
            barMaxWidth: 10,
            itemStyle: {
              color: palette[line.color],
              borderRadius: mood.barRadius,
            },
          }
        : {}),
    }))
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
              lineStyle: { width: 1, type: 'dashed', color: palette.warning },
            },
          ]
        : []
    return {
      animation: false,
      grid: { top: 12, right: 8, bottom: 22, left: 40 },
      tooltip: {
        trigger: 'axis',
        backgroundColor: palette.surfaceSolid,
        borderColor: palette.borderSubtle,
        textStyle: { color: palette.textPrimary, fontSize: 12 },
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
          fontSize: 10,
          hideOverlap: true,
          formatter: (value: number) => clock.format(value),
        },
      },
      yAxis: {
        type: 'value',
        min: 0,
        splitNumber: 3,
        axisLabel: {
          color: palette.textTertiary,
          fontSize: 10,
          formatter: (value: number) => axisNumber(value),
        },
        splitLine: { lineStyle: { type: 'solid', color: palette.chartGrid } },
      },
      series: [...lines, ...threshold],
    }
  },
  [() => props.series, () => props.threshold, () => props.range, locale]
)
</script>

<template>
  <figure class="ops-chart">
    <figcaption class="sr-only">{{ label }}: {{ summary }}</figcaption>
    <div ref="el" :style="{ height: `${height}px` }" aria-hidden="true" />
  </figure>
</template>
