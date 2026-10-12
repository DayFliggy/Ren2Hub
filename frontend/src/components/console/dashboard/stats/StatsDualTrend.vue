<script setup lang="ts">
import { quotaForChart } from '@/utils/quota'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useEChart } from '@/charts/useEChart'
import { areaGradient, lineMood } from '@/charts/themePreset'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import type { FlowPoint } from '@/composables/useDashboard'
import type { StatsComparison, StatsKpi } from '@/composables/useDashboardStats'
import { formatNumber, formatQuota, formatMoney } from '@/utils/format'
import { escapeHtml } from '@/utils/html'

type ChartMode = 'both' | 'consume' | 'requests'

interface TooltipParam {
  axisValueLabel: string
  color: string
  seriesName: string
  value: number
}

const props = defineProps<{
  kpi: StatsKpi | null
  comparison: StatsComparison | null
  flow: FlowPoint[]
  loading?: boolean
  error?: string | null
}>()

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)
const mode = ref<ChartMode>('both')

function defaultFlow(days = 14): FlowPoint[] {
  const now = new Date()
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  return Array.from({ length: days }, (_, index) => {
    const date = new Date(start)
    date.setDate(start.getDate() - (days - index - 1))
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    return {
      date: `${month}-${day}`,
      consume: '0',
      requests: 0,
    }
  })
}

const data = computed(() =>
  props.flow.length > 0 ? props.flow : defaultFlow(14)
)
const modes = computed(() => [
  { key: 'both' as const, label: t('dashboard.trendBoth') },
  { key: 'consume' as const, label: t('dashboard.consumeShort') },
  { key: 'requests' as const, label: t('dashboard.requestsShort') },
])

function deltaStyle(value: number): string {
  return value >= 0
    ? 'background:var(--status-danger-soft);color:var(--status-danger-text)'
    : 'background:var(--status-success-soft);color:var(--status-success-text)'
}

function deltaLabel(value: number): string {
  return `${value >= 0 ? '↑' : '↓'}${Math.abs(value)}%`
}

useEChart(
  el,
  (palette, scale = 1) => {
    const mood = lineMood(palette)
    const showConsume = mode.value === 'both' || mode.value === 'consume'
    const showRequests = mode.value === 'both' || mode.value === 'requests'
    const singleAxis = mode.value !== 'both'
    const consumeLabel = t('dashboard.stats.spend')
    const requestsLabel = t('dashboard.stats.requests')

    return {
      grid: {
        left: (singleAxis ? 46 : 54) * scale,
        right: (singleAxis ? 14 : 54) * scale,
        top: 40 * scale,
        bottom: 28 * scale,
      },
      tooltip: {
        trigger: 'axis',
        backgroundColor: palette.surfaceSolid,
        borderColor: palette.borderSubtle,
        textStyle: { color: palette.textPrimary, fontSize: 12 * scale },
        formatter: (params: TooltipParam[]) => {
          if (!params.length) return ''
          const rows = params
            .map((entry) => {
              const value =
                entry.seriesName === consumeLabel
                  ? formatMoney(entry.value)
                  : formatNumber(entry.value)
              return `<div style="display:flex;align-items:center;gap:0.375rem;margin-top:0.25rem">
                <span style="width:0.5rem;height:0.5rem;border-radius:50%;background:${entry.color}"></span>
                <span style="color:${palette.textSecondary}">${escapeHtml(entry.seriesName)}</span>
                <strong style="margin-left:auto">${escapeHtml(value)}</strong>
              </div>`
            })
            .join('')
          return `<div style="min-width:9.375rem"><strong>${escapeHtml(params[0]!.axisValueLabel)}</strong>${rows}</div>`
        },
      },
      legend: {
        data: [
          ...(showConsume ? [consumeLabel] : []),
          ...(showRequests ? [requestsLabel] : []),
        ],
        top: 0,
        left: 'center',
        textStyle: { color: palette.textTertiary, fontSize: 10 * scale },
        icon: 'circle',
        itemWidth: 12 * scale,
        itemHeight: 12 * scale,
        itemGap: 14 * scale,
        inactiveColor: palette.borderSubtle,
      },
      xAxis: {
        type: 'category',
        data: data.value.map((point) => point.date),
        axisLine: { lineStyle: { color: palette.borderSubtle } },
        axisTick: { show: false },
        axisLabel: {
          color: palette.textTertiary,
          fontSize: 10 * scale,
          interval: Math.floor(data.value.length / 6),
        },
      },
      yAxis: singleAxis
        ? [
            {
              type: 'value',
              splitLine: mood.splitLine,
              axisLabel: {
                color: palette.textTertiary,
                fontSize: 10 * scale,
                formatter: (value: number) =>
                  value >= 1000
                    ? `${Math.round(value / 100) / 10}K`
                    : String(value),
              },
            },
          ]
        : [
            {
              type: 'value',
              name: consumeLabel,
              nameTextStyle: {
                color: palette.textTertiary,
                fontSize: 10 * scale,
              },
              splitLine: mood.splitLine,
              axisLabel: {
                color: palette.textTertiary,
                fontSize: 10 * scale,
                formatter: (value: number) =>
                  value >= 1000
                    ? `${Math.round(value / 100) / 10}K`
                    : String(value),
              },
            },
            {
              type: 'value',
              name: requestsLabel,
              nameTextStyle: {
                color: palette.textTertiary,
                fontSize: 10 * scale,
              },
              splitLine: { show: false },
              axisLabel: {
                color: palette.textTertiary,
                fontSize: 10 * scale,
                formatter: (value: number) =>
                  value >= 1000
                    ? `${Math.round(value / 100) / 10}K`
                    : String(value),
              },
            },
          ],
      series: [
        ...(showConsume
          ? [
              {
                name: consumeLabel,
                type: 'line',
                smooth: mood.line.smooth,
                data: data.value.map((point) => quotaForChart(point.consume)),
                lineStyle: {
                  color: palette.accent,
                  ...mood.line.lineStyle,
                  width: mood.line.lineStyle.width * scale,
                },
                itemStyle: {
                  color: palette.accent,
                  borderColor: palette.surfaceSolid,
                  borderWidth: 2 * scale,
                },
                areaStyle: {
                  color: {
                    type: 'linear',
                    x: 0,
                    y: 0,
                    x2: 0,
                    y2: 1,
                    colorStops: areaGradient(palette, palette.accent),
                  },
                },
                showSymbol: !palette.isDark,
                symbol: mood.line.symbol,
                symbolSize: mood.line.symbolSize * scale,
                yAxisIndex: 0,
              },
            ]
          : []),
        ...(showRequests
          ? [
              {
                name: requestsLabel,
                type: 'line',
                smooth: mood.line.smooth,
                data: data.value.map((point) => point.requests),
                lineStyle: {
                  color: palette.signal,
                  ...mood.line.lineStyle,
                  width: mood.line.lineStyle.width * scale,
                },
                itemStyle: {
                  color: palette.signal,
                  borderColor: palette.surfaceSolid,
                  borderWidth: 2 * scale,
                },
                showSymbol: !palette.isDark,
                symbol: mood.line.symbol,
                symbolSize: mood.line.symbolSize * scale,
                yAxisIndex: singleAxis ? 0 : 1,
              },
            ]
          : []),
      ],
    }
  },
  [data, mode]
)
</script>

<template>
  <ConsoleCard stretch data-stats-dual-trend>
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div
        class="grid w-full min-w-0 grid-cols-2 gap-3 sm:flex sm:w-auto sm:flex-wrap sm:items-center sm:gap-5"
      >
        <div class="min-w-0">
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('dashboard.stats.spend') }}
          </p>
          <div class="mt-1 flex min-w-0 items-center gap-1.5 sm:gap-2">
            <span
              v-if="loading"
              class="inline-block h-7 w-24 animate-pulse rounded bg-[var(--surface-muted)]"
            />
            <strong
              v-else
              class="truncate text-xl font-bold tabular-nums text-[var(--text-primary)] sm:text-2xl"
              data-trend-spend
            >
              {{ kpi ? formatQuota(kpi.totalQuota) : '—' }}
            </strong>
            <span
              v-if="
                comparison?.quotaDelta !== null &&
                comparison?.quotaDelta !== undefined &&
                !loading
              "
              class="shrink-0 rounded px-1 py-0.5 text-[0.625rem] font-semibold tabular-nums sm:px-1.5 sm:text-xs"
              :style="deltaStyle(comparison.quotaDelta)"
              data-trend-spend-delta
            >
              {{ deltaLabel(comparison.quotaDelta) }}
            </span>
          </div>
        </div>

        <div class="hidden h-9 w-px bg-[var(--border-subtle)] sm:block" />

        <div class="min-w-0">
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('dashboard.stats.requests') }}
          </p>
          <div class="mt-1 flex min-w-0 items-center gap-1.5 sm:gap-2">
            <span
              v-if="loading"
              class="inline-block h-7 w-20 animate-pulse rounded bg-[var(--surface-muted)]"
            />
            <strong
              v-else
              class="truncate text-xl font-bold tabular-nums text-[var(--signal)] sm:text-2xl"
              data-trend-requests
            >
              {{ kpi ? formatNumber(kpi.totalRequests) : '—' }}
            </strong>
            <span
              v-if="
                comparison?.requestsDelta !== null &&
                comparison?.requestsDelta !== undefined &&
                !loading
              "
              class="shrink-0 rounded px-1 py-0.5 text-[0.625rem] font-semibold tabular-nums sm:px-1.5 sm:text-xs"
              :style="deltaStyle(comparison.requestsDelta)"
              data-trend-requests-delta
            >
              {{ deltaLabel(comparison.requestsDelta) }}
            </span>
          </div>
        </div>
      </div>

      <div
        class="grid w-full grid-cols-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-0.5 text-xs sm:w-auto"
        :aria-label="t('dashboard.stats.trendMode')"
      >
        <button
          v-for="option in modes"
          :key="option.key"
          type="button"
          class="rounded-lg px-3 py-1.5 font-medium transition-all focus-ring"
          :class="
            mode === option.key
              ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-sm'
              : 'text-[var(--text-tertiary)] hover:text-[var(--text-secondary)]'
          "
          :aria-pressed="mode === option.key"
          :data-trend-mode="option.key"
          @click="mode = option.key"
        >
          {{ option.label }}
        </button>
      </div>
    </div>

    <div
      v-if="loading"
      class="mt-4 h-64 grow animate-pulse rounded-xl bg-[var(--surface-muted)] sm:h-56"
    />
    <EmptyState
      v-else-if="error"
      class="mt-4 grow"
      :title="t('dashboard.loadFailed')"
      :hint="error"
    />
    <div
      v-else
      ref="el"
      class="mt-4 h-64 w-full grow sm:h-56"
      role="img"
      :aria-label="t('dashboard.stats.trendTitle')"
    />
  </ConsoleCard>
</template>
