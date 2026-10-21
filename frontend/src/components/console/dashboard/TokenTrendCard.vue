<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useEChart } from '@/charts/useEChart'
import { areaGradient, lineMood } from '@/charts/themePreset'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import type { UsageSummary } from '@/api/dashboard'
import type { TokenTrendPoint } from '@/composables/useDashboard'
import { formatCompact } from '@/utils/format'
import { escapeHtml } from '@/utils/html'

const props = defineProps<{
  points: TokenTrendPoint[]
  cacheCoverage?: UsageSummary['meta']['cache_coverage'] | null
  loading?: boolean
}>()

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)

function defaultPoints(days = 30): TokenTrendPoint[] {
  const now = new Date()
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  return Array.from({ length: days }, (_, index) => {
    const date = new Date(start)
    date.setDate(start.getDate() - (days - index - 1))
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    return {
      date: `${year}-${month}-${day}`,
      total: 0,
      input: null,
      output: null,
      cache_create: null,
      cache_read: null,
      hit_rate: null,
    }
  })
}

const displayPoints = computed(() =>
  props.points.length > 0 ? props.points : defaultPoints(30)
)

interface TooltipParam {
  dataIndex: number
  seriesName: string
  value: number | null
  color: string
  axisValueLabel: string
}

useEChart(
  el,
  (p, scale = 1) => {
    const mood = lineMood(p)
    const dates = displayPoints.value.map((d) => d.date)
    const labels = {
      input: t('dashboard.tokenTrend.input'),
      output: t('dashboard.tokenTrend.output'),
      cacheCreate: t('dashboard.tokenTrend.cacheCreate'),
      cacheRead: t('dashboard.tokenTrend.cacheRead'),
      hitRate: t('dashboard.tokenTrend.hitRate'),
    }

    // Token series share the left axis; the hit-rate percentage gets its own.
    const tokenSeries = [
      ...(props.points.some((point) => point.input === null)
        ? [
            {
              name: t('dashboard.stats.totalTokens'),
              key: 'total' as const,
              color: p.signal,
            },
          ]
        : []),
      { name: labels.input, key: 'input' as const, color: p.signal },
      { name: labels.output, key: 'output' as const, color: p.success },
      {
        name: labels.cacheCreate,
        key: 'cache_create' as const,
        color: p.warning,
      },
      { name: labels.cacheRead, key: 'cache_read' as const, color: p.accent },
    ].map((s) => ({
      name: s.name,
      type: 'line' as const,
      smooth: mood.line.smooth,
      showSymbol: !p.isDark,
      symbol: mood.line.symbol,
      symbolSize: mood.line.symbolSize * scale,
      yAxisIndex: 0,
      data: displayPoints.value.map((d) => d[s.key]),
      lineStyle: {
        color: s.color,
        ...mood.line.lineStyle,
        width: mood.line.lineStyle.width * scale,
      },
      itemStyle: {
        color: s.color,
        borderColor: p.surfaceSolid,
        borderWidth: 2 * scale,
      },
      areaStyle: {
        color: {
          type: 'linear' as const,
          x: 0,
          y: 0,
          x2: 0,
          y2: 1,
          colorStops: areaGradient(p, s.color),
        },
      },
    }))

    return {
      grid: {
        left: 52 * scale,
        right: 42 * scale,
        top: 26 * scale,
        bottom: 20 * scale,
      },
      legend: {
        top: 0,
        left: 'center',
        itemWidth: 10 * scale,
        itemHeight: 10 * scale,
        itemGap: 12 * scale,
        icon: 'circle',
        textStyle: { color: p.textTertiary, fontSize: 10 * scale },
        inactiveColor: p.borderSubtle,
      },
      tooltip: {
        trigger: 'axis',
        backgroundColor: p.surfaceSolid,
        borderColor: p.borderSubtle,
        textStyle: { color: p.textPrimary, fontSize: 12 * scale },
        formatter: (params: TooltipParam[]) => {
          if (!params.length) return ''
          const rows = params
            .map((s) => {
              const value =
                s.seriesName === labels.hitRate
                  ? s.value === null
                    ? '—'
                    : `${s.value}%`
                  : s.value === null
                    ? '—'
                    : formatCompact(s.value)
              return `<div style="display:flex;align-items:center;gap:0.375rem;margin-top:0.1875rem">
                <span style="width:0.5rem;height:0.5rem;border-radius:0.125rem;background:${s.color}"></span>
                <span style="color:${p.textSecondary}">${escapeHtml(s.seriesName)}:</span>
                <span style="margin-left:auto;font-weight:600">${escapeHtml(value)}</span>
              </div>`
            })
            .join('')
          return `<div style="min-width:11.625rem">
            <div style="font-weight:700;margin-bottom:0.125rem">${escapeHtml(params[0]!.axisValueLabel)}</div>
            ${rows}
          </div>`
        },
      },
      xAxis: {
        type: 'category',
        boundaryGap: false,
        data: dates,
        axisLine: { lineStyle: { color: p.borderSubtle } },
        axisTick: { show: false },
        axisLabel: {
          color: p.textTertiary,
          fontSize: 9 * scale,
          interval: Math.max(0, Math.floor(dates.length / 5) - 1),
        },
      },
      yAxis: [
        {
          type: 'value',
          minInterval: 1,
          splitLine: mood.splitLine,
          axisLabel: {
            color: p.textTertiary,
            fontSize: 9 * scale,
            formatter: (v: number) => formatCompact(v),
          },
        },
        {
          type: 'value',
          min: 0,
          max: 100,
          splitLine: { show: false },
          axisLabel: {
            color: p.support,
            fontSize: 9 * scale,
            formatter: (v: number) => `${v}%`,
          },
        },
      ],
      series: [
        ...tokenSeries,
        {
          name: labels.hitRate,
          type: 'line',
          smooth: mood.line.smooth,
          yAxisIndex: 1,
          showSymbol: !p.isDark,
          symbol: mood.line.symbol,
          symbolSize: mood.line.symbolSize * scale,
          data: displayPoints.value.map((d) => d.hit_rate),
          lineStyle: {
            color: p.support,
            ...mood.line.lineStyle,
            width: mood.line.lineStyle.width * scale,
            type: p.isDark ? 'solid' : 'dashed',
          },
          itemStyle: {
            color: p.surfaceSolid,
            borderColor: p.support,
            borderWidth: 2 * scale,
          },
        },
      ],
    }
  },
  () => displayPoints.value
)
</script>

<template>
  <ConsoleCard
    :title="t('dashboard.tokenTrend.title')"
    stretch
    data-token-trend-card
  >
    <template #action>
      <div class="flex items-center gap-3 text-xs">
        <span class="text-[var(--text-tertiary)]">
          {{ t('dashboard.tokenTrend.avgHitRate') }}
          <span class="font-semibold text-[var(--support)]">—</span>
        </span>
      </div>
    </template>

    <p class="mb-3 text-xs text-[var(--text-tertiary)]" data-cache-coverage>
      {{
        cacheCoverage
          ? t('dashboard.tokenTrend.cacheCoverage', {
              records: cacheCoverage.records,
              read: cacheCoverage.read_records,
              write: cacheCoverage.write_records,
              invalid: cacheCoverage.invalid_records,
            })
          : t('dashboard.tokenTrend.cacheUnknown')
      }}
    </p>
    <div
      v-if="loading"
      class="h-[11.875rem] grow animate-pulse rounded-xl bg-[var(--surface-muted)] sm:h-[12.25rem]"
    />
    <div
      v-else
      ref="el"
      class="h-[11.875rem] w-full grow sm:h-[12.25rem]"
      role="img"
      :aria-label="t('dashboard.tokenTrend.title')"
    />
  </ConsoleCard>
</template>
