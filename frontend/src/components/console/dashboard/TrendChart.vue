<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useEChart } from '@/charts/useEChart'
import ConsoleCard from '@/components/common/ConsoleCard.vue'

const props = withDefaults(
  defineProps<{
    title: string
    headline: string
    delta?: number
    note?: string
    categories: string[]
    series: number[]
    colorRole?: 'accent' | 'signal'
    valueFormatter?: (v: number) => string
  }>(),
  {
    delta: undefined,
    note: '',
    colorRole: 'accent',
    valueFormatter: (v: number) => String(v),
  }
)

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)

const categories = computed(() => props.categories)
const series = computed(() => props.series)

useEChart(
  el,
  (p) => {
    const lineColor = props.colorRole === 'signal' ? p.signalStrong : p.accent
    return {
      grid: { left: 44, right: 12, top: 14, bottom: 26 },
      tooltip: {
        trigger: 'axis',
        backgroundColor: p.surfaceSolid,
        borderColor: p.borderSubtle,
        textStyle: { color: p.textPrimary, fontSize: 12 },
        valueFormatter: (v: number) => props.valueFormatter(v),
      },
      xAxis: {
        type: 'category',
        data: categories.value,
        axisLine: { lineStyle: { color: p.borderSubtle } },
        axisTick: { show: false },
        axisLabel: { color: p.textTertiary, fontSize: 10, interval: 4 },
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: p.borderSubtle, type: 'dashed' } },
        axisLabel: {
          color: p.textTertiary,
          fontSize: 10,
          formatter: (v: number) =>
            v >= 1000 ? `${Math.round(v / 100) / 10}K` : String(v),
        },
      },
      series: [
        {
          type: 'line',
          smooth: true,
          symbol: 'circle',
          symbolSize: 6,
          showSymbol: false,
          data: series.value,
          lineStyle: { color: lineColor, width: 2.5 },
          itemStyle: {
            color: lineColor,
            borderColor: p.surfaceSolid,
            borderWidth: 2,
          },
          areaStyle: {
            color: {
              type: 'linear',
              x: 0,
              y: 0,
              x2: 0,
              y2: 1,
              colorStops: [
                { offset: 0, color: lineColor + '33' },
                { offset: 1, color: lineColor + '05' },
              ],
            },
          },
        },
      ],
    }
  },
  [categories, series]
)
</script>

<template>
  <ConsoleCard>
    <template #action>
      <span
        class="rounded-lg bg-[var(--surface-muted)] px-2.5 py-1 text-xs text-[var(--text-tertiary)]"
      >
        {{ t('dashboard.monthly') }} ▾
      </span>
    </template>
    <p class="text-sm text-[var(--text-tertiary)]">{{ title }}</p>
    <div class="mt-2 flex items-center gap-2.5">
      <p class="text-2xl font-bold tracking-tight text-[var(--text-primary)]">
        {{ headline }}
      </p>
      <span
        v-if="delta !== undefined"
        class="rounded-md px-1.5 py-0.5 text-xs font-semibold"
        :style="
          delta >= 0
            ? 'background:var(--status-danger-soft);color:var(--status-danger-text)'
            : 'background:var(--status-success-soft);color:var(--status-success-text)'
        "
      >
        {{ delta >= 0 ? '↗' : '↘' }} {{ delta > 0 ? '+' : '' }}{{ delta }}%
      </span>
    </div>
    <p
      v-if="note"
      class="mt-1 flex items-center gap-1.5 text-xs text-[var(--text-tertiary)]"
    >
      <span
        class="inline-block h-3.5 w-3.5 rounded-full border border-[var(--border-default)] text-center text-[10px] leading-3"
        >i</span
      >
      {{ note }}
    </p>
    <div ref="el" class="mt-3 h-40 w-full" role="img" :aria-label="title" />
  </ConsoleCard>
</template>
