<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useEChart } from '@/charts/useEChart'
import { lineMood } from '@/charts/themePreset'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import type { HourlyPoint } from '@/composables/useDashboardStats'
import { formatTime } from '@/utils/format'

const props = defineProps<{
  hourly: HourlyPoint[]
  loading?: boolean
  error?: string | null
}>()

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)

// Placeholder axis for an empty response: today's 24 local hours at zero.
function defaultHourly(): HourlyPoint[] {
  const midnight = new Date()
  midnight.setHours(0, 0, 0, 0)
  const start = midnight.getTime() / 1000
  return Array.from({ length: 24 }, (_, i) => ({
    ts: start + i * 3600,
    requests: 0,
    tokens: 0,
    quota: '0',
  }))
}

const data = computed(() =>
  props.hourly.length > 0 ? props.hourly : defaultHourly()
)

// The series is a real timeline over the whole range, so multi-day ranges
// keep the date in each label; formatTime renders "YYYY-MM-DD HH:mm".
const multiDay = computed(() => {
  const points = data.value
  return (
    formatTime(points[0]!.ts).slice(0, 10) !==
    formatTime(points.at(-1)!.ts).slice(0, 10)
  )
})
function hourLabel(ts: number): string {
  return formatTime(ts).slice(multiDay.value ? 5 : 11)
}

const peakHour = computed(() => {
  if (!props.hourly.length) return null
  return props.hourly.reduce((a, b) => (a.requests > b.requests ? a : b))
})

useEChart(
  el,
  (p, scale = 1) => {
    const mood = lineMood(p)
    const values = data.value.map((h) => h.requests)
    const maxVal = Math.max(...values, 1)
    return {
      grid: {
        left: 40 * scale,
        right: 12 * scale,
        top: 12 * scale,
        bottom: 28 * scale,
      },
      tooltip: {
        trigger: 'axis',
        backgroundColor: p.surfaceSolid,
        borderColor: p.borderSubtle,
        textStyle: { color: p.textPrimary, fontSize: 12 * scale },
        axisPointer: p.isDark
          ? { type: 'shadow' }
          : {
              type: 'line',
              lineStyle: {
                color: p.borderSubtle,
                type: [7, 3, 2, 5],
                width: 1 * scale,
              },
            },
      },
      xAxis: {
        type: 'category',
        data: data.value.map((h) => hourLabel(h.ts)),
        axisLine: { lineStyle: { color: p.borderSubtle } },
        axisTick: { show: false },
        axisLabel: {
          color: p.textTertiary,
          fontSize: 9 * scale,
          interval: Math.max(2, Math.floor(data.value.length / 8) - 1),
        },
      },
      yAxis: {
        type: 'value',
        minInterval: 1,
        splitLine: mood.splitLine,
        axisLabel: { color: p.textTertiary, fontSize: 10 * scale },
      },
      series: [
        {
          type: 'bar',
          data: values.map((v) => ({
            value: v,
            itemStyle: {
              color: v > 0 && v === maxVal ? p.accent : `${p.accent}66`,
              borderRadius: mood.barRadius.map((radius) => radius * scale),
            },
          })),
          emphasis: {
            itemStyle: { color: p.accent },
          },
        },
      ],
    }
  },
  [data]
)
</script>

<template>
  <ConsoleCard :title="t('dashboard.stats.hourlyDist')" stretch>
    <template #action>
      <span
        v-if="peakHour && peakHour.requests > 0"
        class="rounded-lg bg-[var(--accent-soft)] px-2.5 py-1 text-xs text-[var(--accent-text)]"
      >
        {{ t('dashboard.stats.peakAt', { hour: formatTime(peakHour.ts) }) }}
      </span>
    </template>

    <!-- h-36 is the floor; `grow` fills the row set by the model table beside it -->
    <div
      v-if="loading"
      class="h-52 grow animate-pulse rounded-xl bg-[var(--surface-muted)] sm:h-44 xl:h-36"
    />
    <EmptyState
      v-else-if="error"
      class="grow"
      :title="t('dashboard.loadFailed')"
      :hint="error"
    />
    <div
      v-else
      ref="el"
      class="h-52 w-full grow sm:h-44 xl:h-36"
      role="img"
      :aria-label="t('dashboard.stats.hourlyDist')"
    />
  </ConsoleCard>
</template>
