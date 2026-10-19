<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useEChart } from '@/charts/useEChart'
import { areaGradient, lineMood } from '@/charts/themePreset'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import { formatAdminOrderAmount } from '@/constants/adminOrders'
import type { AdminOrderDailyPoint } from '@/types/console'

const props = withDefaults(
  defineProps<{
    points: AdminOrderDailyPoint[]
    currency?: string
    loading?: boolean
    partialAmounts?: boolean
  }>(),
  { loading: false, currency: '', partialAmounts: false }
)

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)

const points = computed(() => props.points)
const amountLabel = computed(() =>
  t(
    props.partialAmounts ? 'orders.knownAmountSubtotal' : 'orders.chart.revenue'
  )
)

/**
 * Label density is derived from the window length: 7 days shows every tick,
 * 90 days would otherwise overlap into an unreadable band.
 */
const labelInterval = computed(() => {
  const count = props.points.length
  if (count <= 10) return 0
  return Math.max(1, Math.ceil(count / 12) - 1)
})

/**
 * Ticks render as `MM-DD` in every window. The year is identical across a range
 * that is at most 90 days, so printing it only costs width — and width is what
 * clips here: `boundaryGap: false` seats the first and last points on the grid
 * boundary, so their centred labels overhang it. The sr-only table keeps the
 * full ISO date for assistive tech.
 */
const axisLabels = computed(() =>
  props.points.map((point) => point.date.slice(5))
)

useEChart(
  el,
  (p, scale = 1) => {
    const mood = lineMood(p)
    return {
      grid: {
        left: 8 * scale,
        right: 8 * scale,
        top: 40 * scale,
        bottom: 24 * scale,
        containLabel: true,
      },
      /**
       * Units live in the legend rather than in `yAxis.name`. An axis name
       * defaults to nameLocation:'end', which puts it in the same top strip the
       * legend occupies — the left axis name lands under the legend and the right
       * one overprints it. Carrying the unit here states it once, unambiguously.
       */
      legend: {
        top: 4 * scale,
        right: 0,
        itemWidth: 18 * scale,
        itemHeight: 10 * scale,
        icon: p.isDark ? 'roundRect' : 'rect',
        textStyle: { color: p.textSecondary, fontSize: 11 * scale },
        data: [amountLabel.value, t('orders.paidOrders')],
        formatter: (name: string) =>
          name === amountLabel.value
            ? `${amountLabel.value} (${props.currency || '—'})`
            : t('orders.paidOrders'),
      },
      tooltip: {
        trigger: 'axis',
        backgroundColor: p.surfaceSolid,
        borderColor: p.borderSubtle,
        textStyle: { color: p.textPrimary, fontSize: 12 * scale },
        axisPointer: { type: 'line', lineStyle: { color: p.borderSubtle } },
      },
      xAxis: {
        type: 'category',
        data: axisLabels.value,
        boundaryGap: false,
        axisLine: { lineStyle: { color: p.borderSubtle } },
        axisTick: { show: false },
        axisLabel: {
          color: p.textTertiary,
          fontSize: 10 * scale,
          interval: labelInterval.value,
        },
      },
      // Neither axis carries a `name`: ECharts anchors it at nameLocation:'end'
      // (the top of the axis), which put the right-hand one directly under the
      // legend. The units live in the legend labels instead, where they cannot
      // collide with anything.
      yAxis: [
        {
          type: 'value',
          splitLine: mood.splitLine,
          axisLabel: {
            color: p.textTertiary,
            fontSize: 10 * scale,
            formatter: (v: number) =>
              v >= 1000 ? `${Math.round(v / 100) / 10}K` : String(v),
          },
        },
        {
          type: 'value',
          splitLine: { show: false },
          axisLabel: { color: p.textTertiary, fontSize: 10 * scale },
          minInterval: 1,
        },
      ],
      series: [
        {
          name: amountLabel.value,
          type: 'line',
          smooth: mood.line.smooth,
          showSymbol: !p.isDark,
          symbol: mood.line.symbol,
          symbolSize: mood.line.symbolSize * scale,
          data: points.value.map((point) => point.revenue),
          lineStyle: {
            color: p.accent,
            ...mood.line.lineStyle,
            width: mood.line.lineStyle.width * scale,
          },
          itemStyle: {
            color: p.accent,
            borderColor: p.surfaceSolid,
            borderWidth: 2 * scale,
          },
          tooltip: {
            valueFormatter: (v: number) =>
              formatAdminOrderAmount(v, props.currency),
          },
          areaStyle: {
            color: {
              type: 'linear',
              x: 0,
              y: 0,
              x2: 0,
              y2: 1,
              colorStops: areaGradient(p, p.accent),
            },
          },
        },
        {
          name: t('orders.paidOrders'),
          type: 'line',
          yAxisIndex: 1,
          smooth: mood.line.smooth,
          showSymbol: !p.isDark,
          symbol: mood.line.symbol,
          symbolSize: mood.line.symbolSize * scale,
          data: points.value.map((point) => point.orders),
          lineStyle: {
            color: p.signalStrong,
            ...mood.line.lineStyle,
            width: mood.line.lineStyle.width * scale,
          },
          itemStyle: {
            color: p.signalStrong,
            borderColor: p.surfaceSolid,
            borderWidth: 2 * scale,
          },
        },
      ],
    }
  },
  [points, labelInterval, () => props.currency, () => amountLabel.value]
)
</script>

<template>
  <ConsoleCard :title="t('orders.chart.title')">
    <p
      v-if="partialAmounts"
      role="status"
      class="mb-3 text-xs text-[var(--text-secondary)]"
    >
      {{ t('orders.partialAmountTrend') }}
    </p>
    <!-- The skeleton overlays the canvas host rather than replacing it: a
         hidden host would report 0×0 to echarts.init() on first mount, and the
         chart would only recover on the next resize. -->
    <div class="relative">
      <div
        ref="el"
        class="h-72 w-full"
        role="img"
        :aria-label="t('orders.chart.title')"
      />
      <div
        v-if="loading"
        class="absolute inset-0 animate-pulse rounded-xl bg-[var(--surface-muted)]"
        aria-hidden="true"
      />
    </div>
    <!-- Canvas carries no text for assistive tech, so the same figures are
         reachable as a table. -->
    <table class="sr-only">
      <caption>
        {{
          t('orders.chart.title')
        }}
      </caption>
      <thead>
        <tr>
          <th scope="col">{{ t('orders.chart.date') }}</th>
          <th scope="col">{{ amountLabel }}</th>
          <th scope="col">{{ t('orders.paidOrders') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="point in points" :key="point.date">
          <th scope="row">{{ point.date }}</th>
          <td>{{ formatAdminOrderAmount(point.revenue, currency) }}</td>
          <td>{{ point.orders }}</td>
        </tr>
      </tbody>
    </table>
  </ConsoleCard>
</template>
