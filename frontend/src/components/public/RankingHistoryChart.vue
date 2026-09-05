<script setup lang="ts">
import { ref } from 'vue'
import { useEChart } from '@/charts/useEChart'
import type { RankingHistoryPoint } from '@/api/publicCatalog'
const props = defineProps<{
  points: RankingHistoryPoint[]
  title: string
  percentage?: boolean
}>()
const element = ref<HTMLElement | null>(null)
useEChart(
  element,
  (palette) => {
    const times = [...new Set(props.points.map((point) => point.ts))]
    const names = [...new Set(props.points.map((point) => point.entity))]
    const points = new Map(
      props.points.map((point) => [`${point.ts}|${point.entity}`, point])
    )
    return {
      color: palette.series,
      tooltip: { trigger: 'axis', renderMode: 'richText', confine: true },
      legend: {
        type: 'scroll',
        bottom: 0,
        textStyle: { color: palette.textSecondary },
      },
      grid: { top: 16, left: 12, right: 16, bottom: 48, containLabel: true },
      xAxis: {
        type: 'category',
        data: times.map(
          (ts) => props.points.find((point) => point.ts === ts)?.label ?? ts
        ),
        axisLabel: { color: palette.textSecondary },
      },
      yAxis: {
        type: 'value',
        max: props.percentage ? 100 : undefined,
        axisLabel: {
          color: palette.textSecondary,
          formatter: props.percentage ? '{value}%' : undefined,
        },
        splitLine: { lineStyle: { color: palette.chartGrid } },
      },
      series: names.map((name) => ({
        name,
        type: 'line',
        stack: 'total',
        showSymbol: false,
        areaStyle: { opacity: 0.25 },
        data: times.map((ts) => {
          const point = points.get(`${ts}|${name}`)
          return props.percentage
            ? (point?.share ?? 0) * 100
            : (point?.tokens ?? 0)
        }),
      })),
    }
  },
  [() => props.points, () => props.percentage]
)
</script>

<template>
  <div
    ref="element"
    role="img"
    :aria-label="title"
    class="h-80 w-full min-w-0"
  />
</template>
